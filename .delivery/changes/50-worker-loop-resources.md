# Change — Add Versioned Dashboard Worker-Loop Resources

Milestone: M7 — Worker Loop Integration / Pilot Readiness
Issue: #50
Risk: Medium
Authorized Base: `5e5414817c8a8f573cd5fba80c48622836e5787d`

## Outcome

Ship one deterministic embedded resource package, `cddm-dashboard-resources/v1.0`, containing the Lead, Implementor and QA role triggers plus the `cddm-worker-result/v1` marker documentation and schema.

## Requirements

- The package declares its resource, methodology and result-protocol identities in `manifest.yaml`.
- Backend loads resources by exact versioned profile and rejects missing, malformed or unsupported packages.
- Resources are embedded in the normal server binary and do not depend on current working directory, Docker mounts or Google Drive.
- Every packaged resource is non-empty and hashable; the complete package exposes a deterministic digest.
- Worker-result schema is valid JSON Schema with role-specific result requirements.
- Startup validates the default package before opening the operational server.
- Unit tests cover successful load, deterministic digest, unsupported profile, missing resource and malformed schema.
- This Change materializes the frozen Change Contracts for #51–#54.

## Out of Scope

- workflow command database records;
- marker ingestion or GitHub synchronization changes;
- prompt rendering changes;
- routing, frontend or Host bridge changes;
- MISAK pilot execution.

## HARD HOW

- The versioned path is embedded below `backend/internal/resourcepack/assets/` so installation output contains the exact resources.
- Runtime profile identity is `<package>/v<version>`.
- `cddm-minimal/v2.0` and `cddm-worker-result/v1` are fixed identities for this resource version.
- Runtime does not download triggers from external document systems.
- Schema validation in this Change proves package integrity; semantic marker acceptance belongs to #51.
- Existing legacy prompt and result behavior remains unchanged until dependent Changes consume the package.

## Implementation Freedom

The Worker may choose internal loader types, manifest parsing helpers, digest composition and tests. The Worker may not add external runtime dependencies, change existing route semantics or introduce workflow persistence.

## Verification

- `go test ./...` and `go test -race ./...`;
- existing frontend, extension and Host checks;
- exact-Head CI;
- independent QA;
- package survives normal server build.
