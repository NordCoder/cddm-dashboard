# CDDM Minimal v2.1 — Continuous Autonomy Profile

**Status:** operational profile  
**Base methodology:** CDDM Minimal v2.0  
**Execution environment:** ChatGPT Web + GitHub Connector + GitHub Actions + CDDM Dashboard  
**Unit of delivery:** GitHub Issue  
**Project coordination surface:** one Project Control Issue

## 1. Purpose

This profile allows CDDM Dashboard to operate the routine delivery loop without the Owner manually transferring every prompt between Lead, Implementor and QA chats.

It changes dispatch ownership and session orchestration. It does not weaken the delivery evidence required by CDDM Minimal v2.0.

The following v2.0 invariants remain normative:

- one Issue is one bounded Change;
- GitHub Issue, comments, PR and CI are durable authority;
- one Candidate is identified by repository, Authorized Base and exact PR Head;
- any new commit creates a new Candidate;
- CI and QA are exact-Head evidence;
- QA is independent from implementation;
- the second `Changes Required` verdict requires `## Lead Cycle Review` before a third Candidate;
- shared-surface and WIP constraints remain enforceable;
- merge is allowed only for the exact approved Candidate;
- merge read-back and Issue closure are required for Done;
- ambiguous or conflicting evidence fails closed.

## 2. Execution modes

A Project selects exactly one execution mode.

### 2.1. `manual_owner_dispatch`

This is the compatibility mode matching CDDM Minimal v2.0:

```text
Lead prepares the Issue
→ Owner starts Implementor
→ Owner starts QA
→ Lead directs correction and merge
```

The Owner remains the routine prompt-transfer operator.

### 2.2. `continuous_dashboard_orchestration`

In this mode:

```text
Lead publishes durable authority in GitHub
→ Dashboard derives or accepts a typed action
→ Dashboard queues the action
→ Dashboard creates or selects the required role session
→ Dashboard delivers a correlated Workflow Command
→ worker publishes a correlated GitHub result
→ Dashboard verifies GitHub facts and computes the next route
```

The Owner is not required to approve each routine wave, open each worker chat, copy each prompt or transfer each report.

Continuous mode is bounded by the current product envelope, Project profile, Issue contracts, typed-action vocabulary and existing CDDM gates.

## 3. Roles and authority

## 3.1. Owner

The Owner retains authority for:

- product goals and priorities;
- material changes to user-visible behavior;
- material scope expansion outside the current product envelope;
- architecture decisions explicitly reserved for the Owner;
- subjective visual acceptance when required;
- release authority;
- acceptance of material residual risk;
- enabling, pausing or stopping continuous orchestration.

In continuous mode, the Owner is not required to:

- approve every routine Issue decomposition;
- approve every new wave inside the active product envelope;
- manually start Implementor or QA sessions;
- transfer worker reports between chats;
- approve ordinary implementation corrections;
- perform ordinary exact-approved merges.

A Lead-created Issue does not require retrospective Owner approval merely because the Owner did not pre-authorize that individual Issue. Owner intervention is required only when the Issue crosses a material Owner boundary.

## 3.2. Lead

The Lead owns:

- WHAT and HARD HOW within the active product envelope;
- Issue decomposition and semantic size gates;
- dependencies, shared surfaces, WIP and merge order;
- `## Lead Dispatch`, `## Lead Decision`, `## Lead Cycle Review` and bounded correction authority;
- creation and readiness of routine Issues;
- creation of Waves;
- requests for supported Implementor, QA and Lead actions;
- reconciliation of Candidate, CI and QA facts;
- exact-approved merge through the authorized GitHub transport;
- merge read-back and Wave completion;
- preparation of the next Wave.

The Lead may create and set routine Issues to `status:ready` without prior Owner approval when all of the following are true:

- the work remains inside the current product envelope;
- no reserved Owner decision is required;
- dependencies and shared surfaces are explicit;
- Issue size is valid;
- verification is defined;
- the Issue can be executed through available capabilities.

The Lead must return `owner_required` rather than guessing when a material Owner decision is required.

The Lead does not perform independent QA and does not bypass exact-Head gates.

## 3.3. Dashboard Orchestrator

Dashboard Orchestrator is a normative operational role, not an AI product role.

It may:

- synchronize GitHub facts;
- derive lifecycle and current route;
- validate typed actions;
- maintain durable queues and lane leases;
- create or reuse authorized role sessions according to session policy;
- bind exact ChatGPT conversations to logical lanes;
- create immutable Prompt Plans and Workflow Commands;
- deliver commands through the bounded browser transport;
- correlate worker results by `command_id`;
- verify PR, Head, CI, QA and merge facts;
- supersede stale pending work;
- pause a lane on ambiguity;
- request Owner or Lead attention through a typed route.

It must not:

- invent product scope;
- invent architecture decisions;
- reinterpret arbitrary prose as executable authority;
- modify source code;
- declare a Candidate approved without exact QA evidence;
- merge directly in the current Dashboard generation;
- recover an ambiguous browser send by blindly replaying it;
- read or classify ChatGPT response content.

Dashboard executes only current durable authority and supported typed actions.

## 3.4. Implementor

The Implementor authority remains bounded by the current Issue and latest effective Lead authority.

In continuous mode, each Implementor command normally receives a fresh ChatGPT session. The Implementor may reconstruct prior Change state from GitHub and continue an existing branch/PR, but chat memory is not authority.

The Implementor does not start other workers, merge or redefine WHAT/HARD HOW.

## 3.5. QA

QA remains fresh and independent for every exact Candidate Head.

A QA session is never reused for a later Candidate. A new commit, correction Head or later QA cycle requires a new QA session identity.

QA does not modify the reviewed branch and does not route another worker directly. It publishes one correlated verdict; Dashboard and Lead determine the next action.

## 4. Project Control Issue

Each continuously orchestrated Project has exactly one open Project Control Issue.

Recommended title:

```text
[CDDM] Autonomous Delivery Control
```

Recommended label:

```text
cddm:control
```

The Control Issue:

- identifies the Project and active product envelope;
- records enable/pause/stop decisions;
- receives Project-level Lead commands and results;
- records Wave identities and summaries;
- receives `plan_next_wave` results;
- is excluded from ordinary Implementor and QA routing;
- is not counted as a normal Work Unit;
- does not replace individual Issue contracts.

Material implementation and correction instructions remain in the target work Issue.

## 5. Session policy

The default continuous session policy is:

```yaml
lead: persistent_per_project
implementor: fresh_per_command
qa: fresh_per_exact_head
```

### 5.1. Lead

One persistent Lead conversation is bound to the Project Control lane.

The Lead conversation may be replaced when unavailable, stale or manually rotated, but only one Lead command may be unresolved for a Project at a time.

### 5.2. Implementor

A fresh Implementor conversation is created for each Workflow Command in the initial profile, including correction commands.

The fresh session may continue the existing Issue branch and PR after reconstructing durable state from GitHub.

### 5.3. QA

A fresh QA conversation is created for each exact Candidate Head.

The QA session identity includes the expected reviewed Head. Head drift invalidates the pending QA action and requires a replacement action/session.

## 6. Logical lanes and serialization

### 6.1. Project Lead lane

```text
project:<project_id>:lead
```

Concurrency is exactly `1`.

The lane remains busy from command creation until one terminal result correlated to the same `command_id` is accepted or the command is explicitly failed, superseded or cancelled.

An unrelated Lead comment does not release the lane.

### 6.2. Implementor lane

```text
project:<project_id>:issue:<issue_number>:implementor
```

Concurrency is exactly `1` per Issue.

Different Issues may run in parallel within the Project WIP limit and shared-surface rules.

### 6.3. QA lane

```text
project:<project_id>:issue:<issue_number>:qa:<expected_head>
```

Concurrency is exactly `1` per exact Head.

### 6.4. Merge operations

Merge is a Lead action and uses the Project Lead lane. This prevents two concurrent Lead commands from racing over merge order, Wave state or shared integration surfaces.

## 7. Typed action vocabulary

A typed action is a routing request. It is not a replacement for the target Issue contract and it may not contain arbitrary executable prompts.

The supported v2 action vocabulary is closed.

### 7.1. `dispatch`

Purpose: start a supported worker role on the current durable Issue authority.

Required target:

```text
repository
issue
role = implementor | qa | lead
```

Candidate-bound QA dispatch also requires `expected_head`.

### 7.2. `correct`

Purpose: start an Implementor correction after Lead has published bounded correction authority in the target Issue.

Required target:

```text
repository
issue
role = implementor
```

When a prior Candidate exists, `expected_previous_head` identifies the reviewed Candidate that triggered correction.

### 7.3. `plan_next_wave`

Purpose: ask the Project Lead to evaluate current Project state and prepare the next bounded Wave.

Target is the Project Control Issue. It must not target a normal work Issue.

### 7.4. `merge_candidate`

Purpose: ask the Lead to perform final exact-Candidate merge checks and merge through the authorized GitHub transport.

Required evidence identity:

```text
repository
issue
pr
expected_head
```

The action is invalid unless exact-Head CI and QA gates are already satisfied in synchronized GitHub facts.

Dashboard does not perform the merge directly in this profile.

### 7.5. `hold`

Purpose: stop progression for a bounded Project, Wave or Issue scope because evidence is stale, conflicting, incomplete or unsafe.

A bounded `reason_code` is required. Arbitrary page or chat text must not be persisted as a diagnostic.

### 7.6. `owner_required`

Purpose: request one material Owner decision.

The action identifies the Project or Issue, a bounded decision category and the durable GitHub location containing the decision context.

It pauses only the affected scope unless the decision blocks the whole Project.

## 8. Lead action batches

A Lead may return an ordered action batch through `cddm-worker-result/v2`.

Rules:

- the result is correlated to one Lead Workflow Command;
- actions are processed in listed order;
- each action has a deterministic identity within the result;
- duplicate action identities are invalid;
- unknown actions are invalid;
- role/action mismatches are invalid;
- actions do not inherit omitted repository or Issue identities from chat memory;
- Candidate-bound actions require full exact Head values;
- Dashboard verifies current GitHub facts before materializing each action;
- a stale action is superseded or held, never silently retargeted;
- partial execution is recorded per action;
- re-synchronization is idempotent.

## 9. Waves

A Wave is a Project-level coordination record containing an ordered set of work Issues intended to progress under one Lead planning result.

A Wave has:

```text
wave_id
project_id
control_issue
ordered issue list
creation command_id
state
```

Wave states:

```text
planned
active
waiting
completed
blocked
superseded
```

A Wave becomes `completed` only when every member Issue is `status:done`, its PR merge is verified and no required post-merge gate remains.

When the active Wave completes and no higher-priority correction or blocker is pending, Dashboard enqueues `plan_next_wave` on the Project Lead lane.

The Lead may create the next Issues without routine Owner approval inside the active product envelope.

## 10. Lifecycle mapping

The canonical continuous profile uses:

```text
status:backlog
status:ready
status:implementation
status:qa
status:ready-to-merge
status:done
status:blocked
```

Rules:

- exactly one canonical `status:*` lifecycle label is active on a normal work Issue;
- a Project may define an explicit equivalent repository mapping;
- Dashboard reads repository labels but does not rename them unless a separately authorized action exists;
- historical comments do not override the current lifecycle label;
- conflicting canonical lifecycle labels fail closed;
- the Project Control Issue is marked `cddm:control` and is excluded from ordinary lifecycle dispatch;
- `status:blocked` preserves the blocked Work Unit without inventing an executable route.

Suggested transitions:

```text
status:backlog
→ status:ready
→ status:implementation
→ status:qa
→ status:ready-to-merge
→ status:done
```

Corrections return from `status:qa` or `status:ready-to-merge` to `status:implementation`.

## 11. Continuous workflow

### 11.1. Start or resume Project

```text
continuous mode enabled
→ Dashboard verifies Control Issue and Lead binding
→ Dashboard enqueues plan_next_wave when no active Wave exists
→ Lead creates or updates bounded Issues
→ Lead publishes actions_ready
→ Dashboard validates the Issues and activates the Wave
```

### 11.2. Initial implementation

```text
Issue is status:ready
→ dispatch Implementor
→ fresh Implementor session
→ Candidate-ready result
→ exact PR/Head read-back
→ CI observation
→ dispatch fresh QA for exact Head
```

### 11.3. QA approved

```text
QA approved exact Head
→ Lead merge_candidate action waits for Lead lane
→ Lead verifies exact Head and merges
→ Lead publishes merged result
→ Dashboard verifies PR/main/Issue read-back
→ Issue becomes status:done
```

### 11.4. QA changes required

```text
QA changes_required
→ Lead reviews findings
→ Lead publishes bounded correction authority
→ Lead returns correct action
→ fresh Implementor session continues the Change
→ new Candidate Head
→ fresh QA
```

After the second `Changes Required` verdict, Lead must publish `## Lead Cycle Review` before another correction action can materialize.

### 11.5. QA blocked or inconclusive

Dashboard distinguishes:

- missing or pending CI evidence;
- process/session blocker;
- requirement ambiguity;
- Candidate defect;
- external infrastructure blocker.

A process wait does not automatically create a correction Candidate.

### 11.6. Wave completion

```text
all Wave Issues verified Done
→ Wave completed
→ Project Lead lane receives plan_next_wave
→ Lead creates next bounded Wave
→ cycle continues
```

## 12. Queue priority

Default Project priority order:

1. active safety or authority blockers;
2. QA `Changes Required` correction review;
3. exact-approved merge actions;
4. CI failure reconciliation;
5. current-Wave initial implementation;
6. next-Wave planning.

The Project profile may lower WIP but must not reorder merge/correction work behind speculative next-Wave planning when that would violate integration safety.

## 13. WIP defaults

Recommended initial profile:

```yaml
max_active_work_units: 3
max_parallel_implementors: 3
max_parallel_qa: 3
max_parallel_lead: 1
```

Shared-surface locks and explicit dependencies override available numeric capacity.

## 14. Pause, stop and recovery

### 14.1. Pause

Pause prevents creation of new Workflow Commands. Existing commands remain observable and their results may still be reconciled.

### 14.2. Stop

Stop disables continuous orchestration for the Project. It does not delete Issues, commands, results, Waves or session bindings.

### 14.3. Fail closed

The affected lane or scope is held when:

- current lifecycle is ambiguous;
- Lead authority is missing or conflicting;
- an action contains an unknown field or action type;
- expected Head no longer matches;
- more than one terminal result competes for the same command;
- browser send outcome is ambiguous;
- required session or attachment evidence is unavailable;
- GitHub synchronization is stale;
- merge read-back contradicts a worker claim.

The system must not create a replacement command merely to hide uncertainty.

## 15. Owner escalation categories

`owner_required` is appropriate for:

- product behavior choice;
- material scope increase;
- reserved architecture decision;
- subjective visual acceptance;
- security/privacy/legal risk acceptance;
- release decision;
- material residual-risk acceptance;
- change to the active product envelope.

It is not appropriate for:

- ordinary branch or PR mechanics;
- standard CI failure correction;
- bounded implementation detail inside HARD HOW;
- routine Issue decomposition;
- routine exact-approved merge;
- starting the next Wave inside the current product envelope.

## 16. Adoption from v2.0

An existing Project adopts this profile only through an explicit Project decision recorded in the Control Issue.

Required adoption record:

```text
methodology: cddm-minimal/v2.1
execution_mode: continuous_dashboard_orchestration
control_issue: <number>
product_envelope: <durable GitHub reference>
owner_reserved_decisions: <list or none>
lifecycle_mapping: <canonical or explicit mapping>
session_policy: persistent Lead / fresh Implementor / fresh QA
WIP limits: <values>
auto_merge: false
```

Active v2.0 Workflow Commands and accepted `cddm-worker-result/v1` evidence retain their original identity and semantics.

A Project must not reinterpret an existing v1 result as a v2 action batch.

New v2 commands may begin only after the Project execution profile, Control Issue and compatible Dashboard resource package are active.

## 17. Current implementation boundary

This methodology profile grants authority for continuous orchestration but does not claim that every Dashboard release already implements it.

Until the corresponding scheduler, queue, result protocol and session provisioning Changes are integrated:

- manual controls remain authoritative;
- unsupported typed actions remain blocked;
- `auto_merge` remains false;
- Lead performs merges;
- no component may emulate missing orchestration by parsing arbitrary prose.
