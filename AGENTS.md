# Simulator development

Maintain this project independently from the sibling Terraform Certification Mentor.
Use the real HashiCorp Terraform CLI and a distinct provider identity.
Never add Azure authentication, real cloud provisioning, or fabricated Terraform output.
Keep infrastructure JSON separate from Terraform state and preserve corrupt data on errors.
Verify provider contracts against official HashiCorp documentation.
Before expanding resources, execute the Resource Group milestone in a disposable validation lab.
No learner competence can be inferred from development or agent validation.
Use `python3 scripts/build.py --test` and the mentor's `scripts/validate_simulator.py` for relevant verification.
Keep binary caches, stores, state, plans, and personal logs private; commit dependency lock records.
