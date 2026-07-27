# Change #43 — Preserve Codex Base Provider Layer for Isolated Profiles

Risk: Medium
Issue: #43

## Outcome

Make `cddm -p/--profile` honor Codex's documented profile layering inside the isolated Change Worker without importing unrelated host integrations.

## Root cause

Codex 0.134+ loads configuration in this relevant order:

1. user base `$CODEX_HOME/config.toml`;
2. selected `$CODEX_HOME/<profile>.config.toml`;
3. trusted project `.codex/config.toml`;
4. CLI overrides.

CDDM currently copies the repository `.codex/config.toml` into the isolated Worker user config slot and copies only the named profile file. A profile that references a custom provider declared in the host base config therefore fails with `Model provider <name> not found`; the CDDM project security layer is also incorrectly below the profile instead of above it.

## HARD HOW

- For profile-backed execution, the Worker user `config.toml` is regenerated immediately before `codex exec` from a **sanitized host base subset**.
- The sanitized subset may carry only provider-resolution configuration required by Codex model transport: custom `model_providers.*` tables plus bounded top-level provider-selection/catalog/base-url keys.
- Host MCP servers, hooks, notifications, project trust entries, TUI state and unrelated machine integrations are not imported.
- The generated Worker base config adds trust only for the exact persistent Change worktree so the repository's checked-in `.codex/config.toml` loads as the project layer.
- The named host `<profile>.config.toml` continues to be copied into isolated Worker `CODEX_HOME` and Codex is invoked through `codex exec --profile <name>`.
- The repository `.codex/config.toml` remains authoritative for CDDM Worker permissions/shell environment/security because project config is above the profile layer.
- Provider API-key environment variables remain available to the Codex process itself; repository shell-environment policy continues excluding secret variables from Worker shell tools.
- No host auth/MCP/browser/session content is copied beyond the existing isolated `auth.json` behavior and the selected profile file.

## Verification

- host base `[model_providers.codexsale]` survives into Worker user config;
- profile `model_provider = "codexsale"` is copied and can resolve that inherited provider definition;
- nested provider tables survive;
- unrelated `[mcp_servers.*]`, `[projects.*]`, approval/TUI settings from host base are excluded;
- generated user config trusts only the exact Change worktree;
- profile shim still emits `codex exec --profile <name>` and strips CDDM shim-control environment before the real Codex process;
- no-profile existing runtime path remains unchanged;
- native runtime parity, backend race, frontend and Compose CI remain green.
