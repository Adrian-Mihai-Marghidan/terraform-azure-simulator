# Terraform Azure Simulator

An independent local Terraform provider for Azure-style Resource Group practice.
Version 0.1.1; provider source `terraform.local/learning/azuresim`.
This is a locally distributed identity, not a published registry service.

## Architecture

The actual HashiCorp Terraform CLI owns HCL evaluation, dependency resolution, planning, state, refresh, and lifecycle processing.
This Go provider, implemented with HashiCorp's Terraform Plugin Framework, translates CRUD operations into a persistent local JSON store.
It contains no Azure SDK, authentication flow, or cloud endpoint.
No portal or server process is required: Terraform starts the provider binary.

The JSON store is simulated infrastructure. Terraform state is a separate file managed by Terraform.
Out-of-band `simctl` changes affect the store, and provider reads expose them to genuine Terraform plans.

## Build

Development needs Go 1.24 or newer on Linux/WSL2 or macOS.
Provider development is the development agent's responsibility, not a learner prerequisite.
From this project:

```bash
python3 scripts/build.py --test
```

Use `--go /absolute/path/to/go` for an isolated toolchain and `--output`/`--cache` to choose private build locations.
Commit `go.mod` and `go.sum`; binaries, caches, storage, plans, and state are ignored.
Builds use `-trimpath -buildvcs=false` so checkout paths and Git status do not change package checksums. The Go toolchain and platform still affect binaries; keep installed versions immutable.

The sibling mentor connects with `python3 scripts/setup_simulator.py`.
That script builds this project, installs an unpacked local filesystem mirror, and writes a terminal-local `TF_CLI_CONFIG_FILE`.
The provider source and pinned version are explicit in the learner lab. The mirror has no direct registry fallback.

## Supported configuration

```hcl
terraform {
  required_providers {
    azuresim = {
      source  = "terraform.local/learning/azuresim"
      version = "= 0.1.1"
    }
  }
}

provider "azuresim" {
  storage_path = "${abspath(path.root)}/.simulator/infrastructure.json"
}

resource "azuresim_resource_group" "example" {
  name     = "example-rg"
  location = "eastus"
  tags     = { purpose = "learning" }
}
```

Only `azuresim_resource_group` is implemented.
Names are case-insensitively unique per store; IDs are deterministic local subscription-shaped strings.
Tags update in place. Name or location changes require replacement.
Locations are lowercase labels, not a verified catalogue of Azure regions.
Argument validation is deliberately a small simulator contract, not full Azure validation.
Provider aliases and multiple resource instances are ordinary Terraform configuration; independent stores can be selected explicitly.

## Starter lab preflight

```bash
bin/simctl check-lab --lab /absolute/path/to/learner/lab
```

This emits a JSON object containing the verified `storage_path`, or fails with a diagnostic.
It parses native `.tf` files using HashiCorp HCL v2, requires exactly one unaliased `azuresim` provider with the explicit local source, and rejects modules, Terraform backend/cloud blocks, provider aliases, data/ephemeral sources, JSON configuration, and overrides in this starter exercise. Resource blocks are limited to `azuresim_resource_group` and supplemental built-in `terraform_data`.
The actual `storage_path` is evaluated in a restricted context containing only `path.root` and `abspath`; it must resolve to the lab's `.simulator/infrastructure.json`. As when Terraform runs from the lab or with `-chdir`, `path.root` is `"."`; `abspath` resolves relative paths from that lab directory. Keep the documented `abspath(path.root)` expression so the provider receives an absolute storage path.
Comments and resource/tag strings cannot satisfy or trigger configuration checks.
This is a starter-lab boundary check, not Terraform evaluation: Terraform remains responsible for validating configuration and resolving providers.
The provider itself supports aliases and independent stores outside this deliberately restricted starter preflight.

## Storage operations

```bash
bin/simctl list --storage /absolute/path/.simulator/infrastructure.json
bin/simctl set-tags --storage /absolute/path/.simulator/infrastructure.json --name example-rg --tags '{"purpose":"external-change"}'
bin/simctl delete --storage /absolute/path/.simulator/infrastructure.json --name example-rg
```

Storage requires an explicit non-null schema_version 1 and resource_groups object, with non-null resource fields and string-map tags. Missing fields, null tag values, unknown fields, invalid identities, and malformed JSON fail without changing existing bytes. Only a missing file starts an empty environment. Writes use atomic file replacement and a process file lock.
The storage lock protects local writes; it does not demonstrate Terraform remote backend locking.
Duplicate creation fails instead of adopting existing objects. Missing resources are reported as absent on refresh.
Back up each store privately with its associated Terraform state. Moving either independently can break bindings.
Never point storage_path at Terraform state, and never put state, plans, stores, logs, or credentials in public commits.

## Limitations

No Azure subscription or authentication is needed.
This is not compatible with the official AzureRM provider as a whole.
Only the Resource Group body arguments name, location, and tags resemble a subset of AzureRM syntax; use the distinct resource type and provider source shown above.
No `features {}`, data sources, import, other Azure resources, permissions/RBAC, policies, Azure errors, long-running operations, or network behavior are implemented.
HCP Terraform, remote backends, Vault, ephemeral values, and write-only arguments need official explanations or separate exercises.
Changing storage_path while Terraform manages objects can make Terraform see another environment; preserve the original path across sessions.

See [ROADMAP.md](ROADMAP.md) for implemented versus planned work.

## Official implementation references

- [HashiCorp HCL syntax](https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md) and [Go parser API](https://pkg.go.dev/github.com/hashicorp/hcl/v2/hclparse) for preflight
- [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework)
- [Create](https://developer.hashicorp.com/terraform/plugin/framework/resources/create), [read](https://developer.hashicorp.com/terraform/plugin/framework/resources/read), [update](https://developer.hashicorp.com/terraform/plugin/framework/resources/update), and [delete](https://developer.hashicorp.com/terraform/plugin/framework/resources/delete) contracts
- [Plan modification](https://developer.hashicorp.com/terraform/plugin/framework/resources/plan-modification)
- [Terraform 1.12 filesystem mirrors](https://developer.hashicorp.com/terraform/cli/v1.12.x/config/config-file)

## Sharing and reuse

Original simulator source is licensed under [MIT](LICENSE). Dependencies retain their own terms; see [THIRD_PARTY.md](THIRD_PARTY.md).
This project was developed with Codex assistance and remains independently maintained from the mentor.
Public source: [terraform-azure-simulator](https://github.com/Adrian-Mihai-Marghidan/terraform-azure-simulator). The compatible [v0.1.1 source tag](https://github.com/Adrian-Mihai-Marghidan/terraform-azure-simulator/tree/v0.1.1) identifies this Resource Group implementation. No public provider registry entry or binary release is provided.
The related [Terraform Associate 004 mentor guide](https://github.com/Adrian-Mihai-Marghidan/terraform-associate-004-trainer/blob/main/docs/COMPANION_SIMULATOR.md) identifies setup and the deliberate provider-lock workflow.

Full Resource Group lifecycle/drift validation passed on macOS arm64 with Terraform 1.12.2 and Go 1.27.2. Linux/WSL2 and other platforms remain unverified; supported build targets do not establish runtime success.
Do not infer learner competence or production Azure experience from development checks.
