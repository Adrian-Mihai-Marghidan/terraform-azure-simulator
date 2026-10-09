// Package preflight checks the intentionally restricted starter-lab boundary.
// It does not replace Terraform's configuration evaluation, provider selection,
// dependency resolution, planning, or lifecycle processing.
package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

type LabResult struct {
	StoragePath string `json:"storage_path"`
}

// CheckLab parses native Terraform files as HCL, checks the starter boundary,
// and evaluates only storage_path with path.root and abspath available.
// Variables, arbitrary Terraform functions, and filesystem reads are unavailable.
func CheckLab(lab string) (LabResult, error) {
	if !filepath.IsAbs(lab) {
		return LabResult{}, fmt.Errorf("--lab must be an absolute starter lab directory")
	}
	lab = filepath.Clean(lab)
	entries, err := os.ReadDir(lab)
	if err != nil {
		return LabResult{}, fmt.Errorf("read starter lab: %w", err)
	}
	parser := hclparse.NewParser()
	var providers []*hclsyntax.Block
	files := 0
	requirements := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tf.json") || strings.HasSuffix(name, "override.tf") {
			return LabResult{}, fmt.Errorf("JSON configuration and override files are unsupported in this starter lab: %s", name)
		}
		if !strings.HasSuffix(name, ".tf") || strings.HasPrefix(name, ".") {
			continue
		}
		file, diagnostics := parser.ParseHCLFile(filepath.Join(lab, name))
		if diagnostics.HasErrors() {
			return LabResult{}, fmt.Errorf("invalid Terraform HCL: %s", diagnostics.Error())
		}
		files++
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			return LabResult{}, fmt.Errorf("starter lab requires native Terraform HCL")
		}
		for _, block := range body.Blocks {
			switch block.Type {
			case "data", "ephemeral":
				return LabResult{}, fmt.Errorf("%s blocks require a separate exercise; this starter lab supports Resource Groups and terraform_data", block.Type)
			case "resource":
				if len(block.Labels) != 2 || (block.Labels[0] != "azuresim_resource_group" && block.Labels[0] != "terraform_data") {
					return LabResult{}, fmt.Errorf("starter resources must use azuresim_resource_group or built-in terraform_data")
				}
			case "module":
				return LabResult{}, fmt.Errorf("modules are unsupported in the isolated starter lab")
			case "terraform":
				for _, nested := range block.Body.Blocks {
					if nested.Type == "backend" || nested.Type == "cloud" {
						return LabResult{}, fmt.Errorf("starter lab requires default local state; terraform %s blocks are unsupported", nested.Type)
					}
					if nested.Type == "required_providers" {
						requirements++
						if err := checkRequirements(nested); err != nil {
							return LabResult{}, err
						}
					}
				}
			case "provider":
				if len(block.Labels) != 1 || block.Labels[0] != "azuresim" {
					return LabResult{}, fmt.Errorf("starter lab allows only an unaliased azuresim provider block")
				}
				if _, alias := block.Body.Attributes["alias"]; alias {
					return LabResult{}, fmt.Errorf("provider aliases are unsupported in this starter lab")
				}
				providers = append(providers, block)
			}
		}
	}
	if files == 0 {
		return LabResult{}, fmt.Errorf("choose a lab with native .tf configuration files")
	}
	if requirements != 1 {
		return LabResult{}, fmt.Errorf("starter lab requires one explicit azuresim required_providers declaration; found %d", requirements)
	}
	if len(providers) != 1 {
		return LabResult{}, fmt.Errorf("starter lab requires exactly one unaliased azuresim provider block; found %d", len(providers))
	}
	provider := providers[0]
	if len(provider.Body.Blocks) != 0 || len(provider.Body.Attributes) != 1 {
		return LabResult{}, fmt.Errorf("starter azuresim provider accepts only storage_path")
	}
	attribute, exists := provider.Body.Attributes["storage_path"]
	if !exists {
		return LabResult{}, fmt.Errorf("azuresim provider requires the per-lab storage_path")
	}
	context := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			// Terraform started from this root (including -chdir) exposes
			// path.root as ".". abspath below resolves relative to lab,
			// regardless of the caller's current working directory.
			"path": cty.ObjectVal(map[string]cty.Value{"root": cty.StringVal(".")}),
		},
		Functions: map[string]function.Function{
			"abspath": function.New(&function.Spec{
				Params: []function.Parameter{{Name: "path", Type: cty.String}},
				Type:   function.StaticReturnType(cty.String),
				Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
					path := args[0].AsString()
					if !filepath.IsAbs(path) {
						path = filepath.Join(lab, path)
					}
					absolute, err := filepath.Abs(path)
					if err != nil {
						return cty.NilVal, err
					}
					return cty.StringVal(filepath.ToSlash(absolute)), nil
				},
			}),
		},
	}
	value, diagnostics := attribute.Expr.Value(context)
	if diagnostics.HasErrors() {
		return LabResult{}, fmt.Errorf("storage_path must resolve using only path.root and abspath: %s", diagnostics.Error())
	}
	if value.IsNull() || !value.IsWhollyKnown() || !value.Type().Equals(cty.String) {
		return LabResult{}, fmt.Errorf("storage_path must be a known non-null string")
	}
	expected := filepath.Join(lab, ".simulator", "infrastructure.json")
	if value.AsString() != filepath.ToSlash(expected) {
		return LabResult{}, fmt.Errorf("storage_path must resolve to the isolated lab store %s; found %s", expected, value.AsString())
	}
	return LabResult{StoragePath: expected}, nil
}

func checkRequirements(block *hclsyntax.Block) error {
	if len(block.Labels) != 0 || len(block.Body.Blocks) != 0 || len(block.Body.Attributes) != 1 {
		return fmt.Errorf("starter lab required_providers must declare only azuresim")
	}
	attribute, exists := block.Body.Attributes["azuresim"]
	if !exists {
		return fmt.Errorf("starter lab required_providers must declare only azuresim")
	}
	value, diagnostics := attribute.Expr.Value(nil)
	if diagnostics.HasErrors() || value.IsNull() || !value.IsWhollyKnown() || !value.Type().IsObjectType() {
		return fmt.Errorf("azuresim required_providers must use a static source object")
	}
	if !value.Type().HasAttribute("source") || value.Type().HasAttribute("configuration_aliases") {
		return fmt.Errorf("azuresim requires its explicit local source and no configuration aliases")
	}
	source := value.GetAttr("source")
	if source.IsNull() || !source.Type().Equals(cty.String) || source.AsString() != "terraform.local/learning/azuresim" {
		return fmt.Errorf("azuresim provider source must be terraform.local/learning/azuresim")
	}
	return nil
}
