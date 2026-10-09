package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "infrastructure.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPersistentLifecycleAndExternalTags(t *testing.T) {
	ctx := context.Background()
	s := fixture(t)
	g := ResourceGroup{Name: "mentor-rg", Location: "eastus", Tags: map[string]string{"owner": "learner"}}
	if err := s.Create(ctx, g); err != nil {
		t.Fatal(err)
	}
	other, _ := New(s.Path)
	if err := other.SetTags(ctx, ID(g.Name), map[string]string{"owner": "external"}); err != nil {
		t.Fatal(err)
	}
	read, err := s.Read(ctx, ID(g.Name))
	if err != nil || read.Tags["owner"] != "external" {
		t.Fatalf("%+v: %v", read, err)
	}
	if err := s.Delete(ctx, read.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, read.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Read(ctx, read.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDuplicateDoesNotOverwrite(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	g := ResourceGroup{Name: "Mentor-RG", Location: "eastus"}
	if err := s.Create(ctx, g); err != nil {
		t.Fatal(err)
	}
	g.Name = "mentor-rg"
	if err := s.Create(ctx, g); err == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
}

func TestCorruptionPreserved(t *testing.T) {
	s := fixture(t)
	broken := []byte("{broken")
	if err := os.WriteFile(s.Path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), ResourceGroup{Name: "rg", Location: "eastus"}); err == nil {
		t.Fatal("corruption overwritten")
	}
	actual, _ := os.ReadFile(s.Path)
	if string(actual) != string(broken) {
		t.Fatal("corruption was modified")
	}
}

func TestConcurrentWriters(t *testing.T) {
	s := fixture(t)
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			other, _ := New(s.Path)
			failures <- other.Create(context.Background(), ResourceGroup{Name: fmt.Sprintf("rg-%d", i), Location: "eastus"})
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	groups, err := s.List(context.Background())
	if err != nil || len(groups) != 20 {
		t.Fatalf("lost write: %d, %v", len(groups), err)
	}
}

func TestRejectStatePathsAndInvalidResources(t *testing.T) {
	for _, path := range []string{"relative.json", filepath.Join(t.TempDir(), "terraform.tfstate.json")} {
		if _, err := New(path); err == nil {
			t.Fatalf("accepted unsafe path %s", path)
		}
	}
	for _, name := range []string{"bad/name", "ends.", ""} {
		if err := Validate(name, "eastus"); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if err := Validate("valid", "East US"); err == nil {
		t.Fatal("invalid location accepted")
	}
}

func TestUnknownStorageSchemaRejected(t *testing.T) {
	s := fixture(t)
	if err := os.WriteFile(s.Path, []byte(`{"schema_version":2,"resource_groups":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(context.Background()); err == nil {
		t.Fatal("unknown schema accepted")
	}
}

func TestMalformedExistingDocumentsPreservedByEveryOperation(t *testing.T) {
	validGroup := `{"id":"/subscriptions/local-simulator/resourceGroups/rg","name":"rg","location":"eastus","tags":{"owner":"learner"}}`
	withGroup := func(group string) string {
		return `{"schema_version":1,"resource_groups":{"/subscriptions/local-simulator/resourceGroups/rg":` + group + `}}`
	}
	cases := map[string]string{
		"empty-object":            `{}`,
		"null-document":           `null`,
		"missing-version":         `{"resource_groups":{}}`,
		"null-version":            `{"schema_version":null,"resource_groups":{}}`,
		"missing-map":             `{"schema_version":1}`,
		"null-map":                `{"schema_version":1,"resource_groups":null}`,
		"array-map":               `{"schema_version":1,"resource_groups":[]}`,
		"unknown-schema":          `{"schema_version":2,"resource_groups":{}}`,
		"unknown-document-field":  `{"schema_version":1,"resource_groups":{},"extra":true}`,
		"null-resource":           withGroup(`null`),
		"missing-resource-fields": withGroup(`{}`),
		"missing-tags":            withGroup(`{"id":"/subscriptions/local-simulator/resourceGroups/rg","name":"rg","location":"eastus"}`),
		"null-tags":               withGroup(`{"id":"/subscriptions/local-simulator/resourceGroups/rg","name":"rg","location":"eastus","tags":null}`),
		"null-tag-value":          withGroup(`{"id":"/subscriptions/local-simulator/resourceGroups/rg","name":"rg","location":"eastus","tags":{"owner":null}}`),
		"numeric-tag-value":       withGroup(`{"id":"/subscriptions/local-simulator/resourceGroups/rg","name":"rg","location":"eastus","tags":{"owner":42}}`),
		"unknown-resource-field":  withGroup(`{"id":"/subscriptions/local-simulator/resourceGroups/rg","name":"rg","location":"eastus","tags":{},"extra":true}`),
		"trailing-document":       withGroup(validGroup) + `{}`,
	}
	ctx := context.Background()
	operations := map[string]func(*Store) error{
		"list":   func(s *Store) error { _, err := s.List(ctx); return err },
		"read":   func(s *Store) error { _, err := s.Read(ctx, ID("rg")); return err },
		"create": func(s *Store) error { return s.Create(ctx, ResourceGroup{Name: "new-rg", Location: "eastus"}) },
		"update": func(s *Store) error { return s.SetTags(ctx, ID("rg"), map[string]string{"owner": "external"}) },
		"delete": func(s *Store) error { return s.Delete(ctx, ID("rg")) },
	}
	for name, malformed := range cases {
		t.Run(name, func(t *testing.T) {
			for operation, invoke := range operations {
				t.Run(operation, func(t *testing.T) {
					s := fixture(t)
					if err := os.WriteFile(s.Path, []byte(malformed), 0600); err != nil {
						t.Fatal(err)
					}
					if err := invoke(s); err == nil {
						t.Fatal("accepted malformed existing simulator storage")
					}
					actual, err := os.ReadFile(s.Path)
					if err != nil || string(actual) != malformed {
						t.Fatalf("malformed storage was modified: %q, %v", actual, err)
					}
				})
			}
		})
	}
}

func TestExplicitEmptyDocumentAndEmptyTagsRemainValid(t *testing.T) {
	s := fixture(t)
	if err := os.WriteFile(s.Path, []byte(`{"schema_version":1,"resource_groups":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), ResourceGroup{Name: "rg", Location: "eastus"}); err != nil {
		t.Fatal(err)
	}
	group, err := s.Read(context.Background(), ID("rg"))
	if err != nil || group.Tags == nil || len(group.Tags) != 0 {
		t.Fatalf("valid empty tags lost: %+v, %v", group, err)
	}
}

func TestParseTagsRejectsNullAndNonStringValues(t *testing.T) {
	for _, invalid := range []string{`null`, `[]`, `{"owner":null}`, `{"owner":42}`, `{"owner":false}`, `{"owner":{}}`, `{} {}`} {
		if _, err := ParseTags([]byte(invalid)); err == nil {
			t.Fatalf("accepted malformed tags %s", invalid)
		}
	}
	for _, valid := range []string{`{}`, `{"owner":"","team":"learner"}`} {
		if tags, err := ParseTags([]byte(valid)); err != nil || tags == nil {
			t.Fatalf("rejected valid tags %s: %v", valid, err)
		}
	}
}
