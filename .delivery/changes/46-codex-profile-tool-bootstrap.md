# Change #46 — Make Codex Worker Tool Bootstrap Deterministic

Risk: Medium
Issue: #46

## Outcome

Ensure profile-backed isolated CDDM Change Workers retain the shell/file execution capabilities required to implement and verify repository Changes, without inheriting arbitrary host Codex features or weakening the `cddm-worker` security boundary.

## Evidence / Root Cause

A direct Codex 0.145.0 execution in the Issue #19 worktree using the real `codexsale` profile, `--strict-config`, and `default_permissions="cddm-worker"` successfully executed `pwd`, created a file, and read it back. Therefore `cddm-worker` permissions do not suppress shell/file tooling.

The CDDM profile-backed path differs because the profile shim regenerates isolated Worker `$CODEX_HOME/config.toml` from a sanitized host base containing provider-resolution settings only. That intentionally drops host `[features]`. The trusted repository `.codex/config.toml` currently enables `goals` but does not explicitly enable the Codex execution features present in the working normal host environment. The profile-backed isolated Worker can therefore resolve the custom provider while lacking the tool-host capabilities needed by the Change Worker.

The codex.sale `/v1/responses` provider path and `wire_api = "responses"` are already proven capable of function/tool calls and are not changed by this Change. The separate model-catalog response-shape warning is non-blocking and out of scope.

## HARD HOW

### 1. CDDM owns Worker tool feature activation

The repository project config is the authoritative place for CDDM Worker execution features.

Explicitly enable only the minimum Codex features required for the established Change Worker implementation loop:

- `shell_tool`;
- `unified_exec`;
- `code_mode_host`;
- existing `goals` remains enabled.

Do not copy or inherit the host `[features]` table wholesale.

### 2. Preserve profile/base/project layering

Keep the #43 layering contract unchanged:

1. isolated Worker user config contains only the sanitized provider-resolution base plus exact-worktree trust;
2. the selected named Codex profile is copied into isolated `CODEX_HOME`;
3. trusted repository `.codex/config.toml` remains the higher project layer;
4. explicit CLI model/reasoning/permission overrides remain highest where currently defined.

Provider selection and Worker feature activation are separate concerns.

### 3. Preserve the Worker security boundary

Do not weaken or replace `default_permissions="cddm-worker"`.

Retain:

- writes limited to the active Change worktree;
- `.git` read-only to the Worker;
- credential paths denied;
- Worker shell network disabled;
- GitHub/GH credentials excluded from Worker shell environment;
- Host-only commit/push/PR authority;
- no `<all filesystem>` or unrestricted `workspace-write` fallback.

Feature activation makes approved local tools available; permissions continue to constrain what those tools can do.

### 4. Host features remain isolated

The sanitized host base must continue excluding unrelated host feature/integration state, including browser/computer-use, MCP/plugin, remote integration, multi-agent, hooks/TUI/project settings, and any other host `[features]` values not explicitly owned by repository project config.

Do not broaden the #43 safe base allowlist to copy arbitrary `[features]`.

### 5. Compatibility and failure semantics

- Existing no-profile Worker behavior remains valid.
- Existing custom-provider/profile resolution remains valid.
- Failure to load required project feature configuration remains fail-closed rather than silently switching sandbox modes.
- No Candidate/runtime-state/V2/V3 semantics change.
- No product M6 behavior changes.

## Implementation Freedom

The Worker may choose the smallest test/helper organization needed to verify the project feature contract and profile layering. It may refactor nearby config tests only when required for deterministic coverage.

The Worker may not redesign Codex profile layering, import general host configuration, change provider endpoints, relax `cddm-worker`, or alter Candidate publication semantics.

## Verification

Automated evidence must cover:

- `.codex/config.toml` explicitly enables `shell_tool`, `unified_exec`, `code_mode_host`, and `goals`;
- existing `cddm-worker` filesystem/network restrictions are unchanged;
- sanitized host base still preserves custom `model_providers.*` required by `codexsale`;
- sanitized host base still excludes host `[features]` and unrelated host integrations;
- profile-backed command still executes through `codex exec --profile <name>` with existing explicit model/reasoning/permission overrides;
- existing native runtime/profile tests pass;
- backend race, frontend and Compose CI remain green.

Operational acceptance before resuming product Change #19:

- run one real isolated CDDM profile-backed Worker turn with `codexsale` in an owned Change worktree;
- prove it performs both a shell command and an in-worktree file edit/read under `cddm-worker`;
- prove Worker shell network/GitHub delivery authority remains unavailable.
