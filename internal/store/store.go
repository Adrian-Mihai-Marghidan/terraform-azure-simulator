// Package store owns simulated infrastructure, independently of Terraform state.
// File locking is supported on Linux (including WSL2) and macOS.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

var ErrNotFound = errors.New("simulated resource group does not exist")
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.()-]{0,89}$`)
var locationPattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,63}$`)

type ResourceGroup struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags"`
}

// UnmarshalJSON enforces the persisted Resource Group shape. encoding/json
// otherwise accepts null into strings as their zero value, hiding corruption.
func (g *ResourceGroup) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       *string            `json:"id"`
		Name     *string            `json:"name"`
		Location *string            `json:"location"`
		Tags     map[string]*string `json:"tags"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if raw.ID == nil || raw.Name == nil || raw.Location == nil || raw.Tags == nil {
		return fmt.Errorf("stored resource requires non-null id, name, location, and tags")
	}
	tags := make(map[string]string, len(raw.Tags))
	for key, value := range raw.Tags {
		if value == nil {
			return fmt.Errorf("stored tag %q must be a non-null string", key)
		}
		tags[key] = *value
	}
	*g = ResourceGroup{ID: *raw.ID, Name: *raw.Name, Location: *raw.Location, Tags: tags}
	return nil
}

// ParseTags rejects null values instead of encoding/json silently turning
// them into empty strings. It is also used by the out-of-band simulator CLI.
func ParseTags(data []byte) (map[string]string, error) {
	var raw map[string]*string
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, fmt.Errorf("tags must be a JSON object of non-null strings")
	}
	tags := make(map[string]string, len(raw))
	for key, value := range raw {
		if value == nil {
			return nil, fmt.Errorf("tag %q must be a non-null string", key)
		}
		tags[key] = *value
	}
	return tags, nil
}

type document struct {
	SchemaVersion  int                      `json:"schema_version"`
	ResourceGroups map[string]ResourceGroup `json:"resource_groups"`
}

type Store struct{ Path string }

func ID(name string) string {
	return "/subscriptions/local-simulator/resourceGroups/" + strings.ToLower(name)
}

func Validate(name, location string) error {
	if !namePattern.MatchString(name) || strings.HasSuffix(name, ".") {
		return fmt.Errorf("name must be 1-90 ASCII letters, digits, _, -, ., (, or ); start with a letter or digit and do not end with a period")
	}
	if !locationPattern.MatchString(location) {
		return fmt.Errorf("location must be a lowercase region label such as eastus; the simulator does not validate Azure's region catalogue")
	}
	return nil
}

func New(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("storage_path must be an absolute path to a simulator JSON file")
	}
	path = filepath.Clean(path)
	name := strings.ToLower(filepath.Base(path))
	if !strings.HasSuffix(name, ".json") || strings.Contains(name, "tfstate") || strings.Contains(name, "tfplan") {
		return nil, fmt.Errorf("storage_path must be a .json simulator file, separate from Terraform state and plans")
	}
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".terraform" {
			return nil, fmt.Errorf("simulator storage must not be placed under .terraform")
		}
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("storage_path must be a regular file, not a symlink or directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &Store{Path: path}, nil
}

func (s *Store) transaction(ctx context.Context, fn func(*document) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return fmt.Errorf("create simulator storage directory: %w", err)
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open simulator storage lock: %w", err)
	}
	defer lock.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("lock simulator storage: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("simulator storage is busy; timed out after 5 seconds")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	var data document
	file, err := os.Open(s.Path)
	if err == nil {
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&data)
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = fmt.Errorf("unexpected trailing JSON")
			}
		}
		file.Close()
		if err != nil {
			return fmt.Errorf("invalid simulator storage %s: %w; preserve the file and investigate", s.Path, err)
		}
		if data.SchemaVersion != 1 || data.ResourceGroups == nil {
			return fmt.Errorf("unsupported simulator storage schema; expected schema_version 1 and resource_groups")
		}
		for id, group := range data.ResourceGroups {
			if id != ID(group.Name) || group.ID != id {
				return fmt.Errorf("invalid resource identity in simulator storage: %s", id)
			}
			if err := Validate(group.Name, group.Location); err != nil {
				return fmt.Errorf("invalid stored resource %s: %w", id, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read simulator storage: %w", err)
	} else {
		// Only a nonexistent file is an empty environment. Existing documents
		// must supply their schema and map explicitly, including an empty map.
		data = document{SchemaVersion: 1, ResourceGroups: map[string]ResourceGroup{}}
	}
	changed, err := fn(&data)
	if err != nil || !changed {
		return err
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.Path), ".simulator-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(append(encoded, '\n')); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp.Name(), s.Path)
}

func (s *Store) Create(ctx context.Context, group ResourceGroup) error {
	if err := Validate(group.Name, group.Location); err != nil {
		return err
	}
	group.ID = ID(group.Name)
	if group.Tags == nil {
		group.Tags = map[string]string{}
	}
	return s.transaction(ctx, func(data *document) (bool, error) {
		if _, exists := data.ResourceGroups[group.ID]; exists {
			return false, fmt.Errorf("resource group %q already exists in this simulator store; choose another name or reconcile its ownership", group.Name)
		}
		data.ResourceGroups[group.ID] = group
		return true, nil
	})
}

func (s *Store) Read(ctx context.Context, id string) (ResourceGroup, error) {
	var group ResourceGroup
	err := s.transaction(ctx, func(data *document) (bool, error) {
		var exists bool
		group, exists = data.ResourceGroups[id]
		if !exists {
			return false, ErrNotFound
		}
		return false, nil
	})
	return group, err
}

func (s *Store) SetTags(ctx context.Context, id string, tags map[string]string) error {
	if tags == nil {
		tags = map[string]string{}
	}
	return s.transaction(ctx, func(data *document) (bool, error) {
		group, exists := data.ResourceGroups[id]
		if !exists {
			return false, ErrNotFound
		}
		group.Tags = tags
		data.ResourceGroups[id] = group
		return true, nil
	})
}

func (s *Store) Delete(ctx context.Context, id string) error {
	return s.transaction(ctx, func(data *document) (bool, error) {
		if _, exists := data.ResourceGroups[id]; !exists {
			return false, nil
		}
		delete(data.ResourceGroups, id)
		return true, nil
	})
}

func (s *Store) List(ctx context.Context) ([]ResourceGroup, error) {
	groups := []ResourceGroup{}
	err := s.transaction(ctx, func(data *document) (bool, error) {
		for _, group := range data.ResourceGroups {
			groups = append(groups, group)
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
		return false, nil
	})
	return groups, err
}
