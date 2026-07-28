# Change — Harden and Document the Integrated Dashboard Worker Loop

Milestone: M7 — Worker Loop Integration / Pilot Readiness
Issue: #54
Risk: High
Authorized Base: current `main` after #53

## Outcome

Prove the integrated Dashboard worker loop through combined fixtures and current-main verification, reconcile canonical documentation, and publish the operator handoff required to declare `PILOT READY` without starting the real MISAK pilot.

## Requirements

- Add combined fixtures for Candidate ready, QA correction, MISAK #149 process blocker, stale QA and conflicting terminal results.
- Add recovery coverage for Dashboard restart while awaiting result, comment arrival during downtime, duplicate sync, delivered prompt without result and terminal marker before the next poll.
- Run full backend, frontend, extension, Host, Docker and packaging checks.
- Reconcile PRODUCT, PRINCIPLES, ROADMAP, README and browser-delivery documentation with actual merged behavior.
- Provide one clean-checkout bootstrap/start path, environment and Project config examples, resource validation, health/readiness checks, pilot guide and troubleshooting.
- Publish exact integrated Issues/PRs, final main, database migration, CI and combined QA evidence.
- Do not start or modify `NordCoder/misak-website#140`.

## Out of Scope

- real pilot execution;
- automatic merge by default;
- Google Drive synchronization;
- universal workflow engine;
- multiple executor implementations;
- migration of all historical comments.

## HARD HOW

- Final readiness is evidence-based, not a label alone.
- All preceding Changes must be merged in dependency order before combined-main verification.
- Combined QA uses current main and the packaged resources.
- Known limitations may remain only when non-blocking for manual-fresh-QA pilot mode.
- M6 documentation debt is corrected as part of canonical reconciliation.

## Verification

- full CI on this exact Head;
- independent Change QA;
- combined-main CI;
- fresh combined integration QA;
- successful Pilot Readiness Check for a configured fixture Project;
- no unresolved protocol blocker.
