package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const requirements = `terraform {
  required_providers {
    azuresim = {
      source = "terraform.local/learning/azuresim"
      version = "= 0.1.1"
    }
  }
}
`
const validProvider = `provider "azuresim" {
  storage_path = "${abspath(path.root)}/.simulator/infrastructure.json"
}
`

func writeLab(t *testing.T, files map[string]string) string {
	t.Helper()
	lab := t.TempDir()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(lab, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return lab
}

func TestValidLabAcceptsCommentsAndFormatting(t *testing.T) {
	lab := writeLab(t, map[string]string{
		"versions.tf": requirements,
		"main.tf": `# backend cloud module azurerm azapi are harmless comment text.
/* provider "azurerm" {} */
provider "azuresim" {
  storage_path /* comments do not change the attribute */ = "${ abspath( path.root ) }/.simulator/infrastructure.json"
}
resource "azuresim_resource_group" "lesson" {
  name = "mentor-rg"
  location = "eastus"
  tags = { team = "cloud", description = "backend module azapi" }
}
`,
	})
	result, err := CheckLab(lab)
	if err != nil {
		t.Fatal(err)
	}
	if result.StoragePath != filepath.Join(lab, ".simulator", "infrastructure.json") {
		t.Fatalf("unexpected storage %q", result.StoragePath)
	}
	if _, err := os.Stat(filepath.Join(lab, ".simulator")); !os.IsNotExist(err) {
		t.Fatal("configuration preflight created infrastructure storage")
	}
}

func TestRejectUnsafeLabConfigurations(t *testing.T) {
	cases := map[string]string{
		"comment-only-storage": `provider "azuresim" {
  # storage_path = "${abspath(path.root)}/.simulator/infrastructure.json"
  storage_path = "/tmp/shared/infrastructure.json"
}
`,
		"conditional-path-root-spoof": strings.Replace(validProvider, `"${abspath(path.root)}/.simulator/infrastructure.json"`, `path.root == "." ? "/tmp/unrelated-store.json" : "${abspath(path.root)}/.simulator/infrastructure.json"`, 1),
		"relative-path-root":          strings.Replace(validProvider, `"${abspath(path.root)}/.simulator/infrastructure.json"`, `"${path.root}/.simulator/infrastructure.json"`, 1),
		"shared-store":                strings.Replace(validProvider, `/.simulator/infrastructure.json`, `/../shared/infrastructure.json`, 1),
		"aliased": strings.Replace(validProvider, `storage_path`, `alias = "shared"
  storage_path`, 1),
		"multiple-provider-blocks": validProvider + validProvider,
		"real-provider-block":      validProvider + `provider "azurerm" {}`,
		"implicit-real-provider":   validProvider + `resource "azurerm_resource_group" "real" {}`,
		"remote-state-data": validProvider + `data "terraform_remote_state" "remote" {
  backend = "http"
  config = { address = "https://example.test/state" }
}`,
		"unsupported-data":      validProvider + `data "azuresim_resource_group" "lookup" {}`,
		"unsupported-ephemeral": validProvider + `ephemeral "azuresim_unknown" "temporary" {}`,
		"module":                validProvider + `module "cloud" { source = "./module" }`,
		"local-backend-block": validProvider + `terraform {
  backend "local" {}
}`,
		"remote-backend": validProvider + `terraform {
  backend "azurerm" {}
}`,
		"cloud-block": validProvider + `terraform {
  cloud {}
}`,
		"variable-storage": strings.Replace(validProvider, `"${abspath(path.root)}/.simulator/infrastructure.json"`, `var.storage`, 1),
		"file-function":    strings.Replace(validProvider, `"${abspath(path.root)}/.simulator/infrastructure.json"`, `file("unexpected.txt")`, 1),
		"null-storage":     strings.Replace(validProvider, `"${abspath(path.root)}/.simulator/infrastructure.json"`, `null`, 1),
		"no-storage":       `provider "azuresim" {}`,
		"missing-provider": `resource "azuresim_resource_group" "lesson" {}`,
		"provider-extra-attribute": strings.Replace(validProvider, `storage_path`, `features = {}
  storage_path`, 1),
		"invalid-hcl": `provider "azuresim" { storage_path =`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			lab := writeLab(t, map[string]string{"versions.tf": requirements, "main.tf": body})
			if _, err := CheckLab(lab); err == nil {
				t.Fatal("accepted unsafe starter configuration")
			}
		})
	}
}

func TestRejectForeignRequiredProviderIdentity(t *testing.T) {
	cases := map[string]string{
		"real-source":        strings.Replace(requirements, "terraform.local/learning/azuresim", "hashicorp/azurerm", 1),
		"implicit-source":    strings.Replace(requirements, `source = "terraform.local/learning/azuresim"`, "", 1),
		"foreign-local-name": strings.Replace(requirements, "azuresim =", "azurerm =", 1),
		"configuration-aliases": strings.Replace(requirements, `version = "= 0.1.1"`, `version = "= 0.1.1"
      configuration_aliases = []`, 1),
		"missing-requirements": "",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			lab := writeLab(t, map[string]string{"versions.tf": source, "main.tf": validProvider})
			if _, err := CheckLab(lab); err == nil {
				t.Fatal("accepted unsupported provider requirement")
			}
		})
	}
}

func TestRejectOverrideAndJSONFiles(t *testing.T) {
	for _, name := range []string{"override.tf", "settings_override.tf", "alternate.tf.json", "override.tf.json"} {
		t.Run(name, func(t *testing.T) {
			lab := writeLab(t, map[string]string{"versions.tf": requirements, "main.tf": validProvider, name: "{}"})
			if _, err := CheckLab(lab); err == nil {
				t.Fatal("accepted override or JSON configuration")
			}
		})
	}
}

func TestRequireAbsoluteLabAndNativeConfiguration(t *testing.T) {
	if _, err := CheckLab("relative"); err == nil {
		t.Fatal("accepted relative lab")
	}
	if _, err := CheckLab(t.TempDir()); err == nil {
		t.Fatal("accepted empty lab")
	}
}

func TestBuiltInTerraformDataMaySupplementLab(t *testing.T) {
	lab := writeLab(t, map[string]string{"versions.tf": requirements, "main.tf": validProvider + `resource "terraform_data" "supplement" {
  input = "local-value"
}`})
	if _, err := CheckLab(lab); err != nil {
		t.Fatal(err)
	}
}
