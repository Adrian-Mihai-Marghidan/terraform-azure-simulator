package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"terraform-azure-simulator/internal/preflight"
	"terraform-azure-simulator/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Simulator:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: simctl check-lab --lab ABSOLUTE_LAB_PATH | list|set-tags|delete --storage ABSOLUTE_JSON_PATH [--name NAME --tags JSON]")
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	path := flags.String("storage", "", "absolute simulator JSON path (never Terraform state)")
	lab := flags.String("lab", "", "absolute starter simulator lab path")
	name := flags.String("name", "", "resource group name")
	tagsJSON := flags.String("tags", "", "JSON object of string tags")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if command == "check-lab" {
		if *path != "" || *name != "" || *tagsJSON != "" {
			return fmt.Errorf("check-lab accepts only --lab")
		}
		result, err := preflight.CheckLab(*lab)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if *lab != "" {
		return fmt.Errorf("--lab is only supported by check-lab")
	}
	s, err := store.New(*path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch command {
	case "list":
		groups, err := s.List(ctx)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(groups)
	case "set-tags":
		if *name == "" || *tagsJSON == "" {
			return fmt.Errorf("set-tags requires --name and --tags")
		}
		tags, err := store.ParseTags([]byte(*tagsJSON))
		if err != nil {
			return fmt.Errorf("invalid --tags: %w", err)
		}
		return s.SetTags(ctx, store.ID(*name), tags)
	case "delete":
		if *name == "" {
			return fmt.Errorf("delete requires --name")
		}
		return s.Delete(ctx, store.ID(*name))
	default:
		return fmt.Errorf("unknown simulator command %q", command)
	}
}
