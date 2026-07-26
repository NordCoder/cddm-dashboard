# Change — Bootstrap CDDM Codex Minimal

Milestone: pre-M6 delivery bootstrap
Risk: Medium
Issue: #16

## Outcome

The repository can be opened as a trusted Codex project and used to execute the first M6 Change without additional process-design work.

## Requirements

- Root `AGENTS.md` provides concise repository-specific execution rules.
- `.delivery/PRODUCT.md`, `.delivery/PRINCIPLES.md`, and `.delivery/ROADMAP.md` provide bounded canonical context.
- Project Codex configuration applies safe interactive workspace defaults without pinning one task model.
- Repo-local implementation, review, investigation, and CI-repair skills are available on demand.
- The repository distinguishes the new development methodology from the existing Supervisor workflow protocol implemented by the product.
- M6 has explicit Outcome, Change dependencies, safe parallel waves, and Exit Gate.
- Existing Stage 1–5 runtime behavior is unchanged.

## Out of Scope

- M6 product implementation;
- removal or redesign of the Supervisor event protocol;
- credentials or machine-specific configuration;
- automatic merge or unattended execution;
- broad CI redesign.

## Verification

- inspect all added configuration and Markdown for internal consistency;
- validate project TOML syntax;
- confirm no product/runtime source is changed;
- run the existing full local verification baseline where practical;
- require exact-Head CI on the published bootstrap Candidate.
