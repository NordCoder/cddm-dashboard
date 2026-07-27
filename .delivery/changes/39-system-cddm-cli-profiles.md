# Change #39 — Promote `cddm` to system-level multi-repository CLI

## Outcome

Promote the compiled Go Host runtime to a system-level `cddm` CLI that can be invoked from any CDDM repository or from anywhere through a named profile.

## Public CLI

```text
cddm [-C <repo>] [-p <profile>] [--model <model>] [--reasoning <effort>] <command> ...
```

Repository resolution precedence:

1. explicit `-C <path>` / `--repo <path>`;
2. `repo` stored in the selected profile;
3. Git top-level containing the caller's current working directory.

The selected repository is canonicalized to an absolute Git top-level before runtime state/worktree operations. Running outside a Git repository without `-C` or a profile with `repo` fails with a clear error.

## Profiles

Profiles are user-level operator configuration stored outside repositories under the XDG config home (`$XDG_CONFIG_HOME/cddm/config.json`, default `~/.config/cddm/config.json`).

A profile may contain:

- `repo`: repository path;
- `model`: default Codex model;
- `reasoning`: default reasoning effort.

Commands:

```text
cddm profile set <name> [--repo <path>] [--model <model>] [--reasoning <effort>]
cddm profile list
cddm profile show <name>
cddm profile remove <name>
```

`profile set` without `--repo` resolves the current Git repository when possible. Profile writes are atomic and create user config directories as needed.

## Model / reasoning precedence

For `start`, `resume`, and `rotate`:

1. explicit `--model` / `--reasoning` CLI flags;
2. selected profile defaults;
3. existing runtime semantics/defaults.

`start` retains `gpt-5.6-terra / medium` when neither CLI nor profile overrides are present. `resume` and `rotate` retain existing state-aware fallback behavior.

Legacy positional `[model] [reasoning]` remains accepted during this transition, but new help/documentation uses flags.

## Installation

Add a user installer that builds the current repository revision and installs the executable as:

```text
~/.local/bin/cddm
```

or `$CDDM_INSTALL_DIR/cddm` when explicitly overridden. No binary is committed to Git.

The repository-local launcher may remain for compatibility, but `cddm` is the primary documented interface.

## Safety / compatibility

- No M6 product behavior changes.
- Runtime state version remains 4.
- No change to Candidate/V2/V3 authority.
- System-level invocation must never accidentally bind runtime state to the installation directory; all runtime paths derive from the resolved target repository.
- Profiles must not contain or expose credentials.
- Existing `CDDM_REPO_ROOT`, `CDDM_COLOR`, `NO_COLOR`, and current commands remain compatible where practical.

## Verification

- parser tests for global flags before/after profile selection;
- repository resolution from nested working directory, explicit `-C`, and profile;
- explicit repo overrides profile repo while profile model/reasoning defaults still apply;
- CLI model/reasoning overrides profile defaults;
- profile CRUD + atomic config persistence;
- malformed/unknown profiles fail closed;
- current start/resume/rotate positional compatibility;
- install script builds `cddm` with exact revision;
- system binary invocation from a second repository resolves that repository, not the source/build repository;
- existing native runtime parity tests remain green;
- backend/race/frontend/compose CI.
