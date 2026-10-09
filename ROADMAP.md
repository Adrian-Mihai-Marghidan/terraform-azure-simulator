# Simulator roadmap

## Milestone 1: Resource Group

Implemented: distinct Framework provider; persistent local JSON storage; Resource Group create/read/tag-update/delete; deterministic IDs; replacement for name/location changes; out-of-band tag mutation/deletion CLI; atomic writes, process locking, and corruption diagnostics; local filesystem-mirror installation; HCL AST starter-lab preflight; strict persisted schema/tag decoding with malformed-byte preservation regression tests.

Provider 0.1.1 fixes persisted missing/null schema acceptance. Earlier 0.1.0 packages are preserved; do not overwrite same-version mirror packages with changed binaries.

Execution status is recorded in the mentor's validation record and private development evidence. Do not infer passing runtime behavior from this implementation list.

## Future work

No additional Azure resource types are part of the current milestone.
Resource Group lookup/import, additional types, and broader Azure behavior remain unimplemented.
Select later work from a concrete curriculum need and verify its provider semantics before teaching it.
Remote locking, HCP Terraform, Vault, and Azure service fidelity are separate topics, not features implied by this local provider.
