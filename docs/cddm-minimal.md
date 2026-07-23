# CDDM Minimal

CDDM Minimal is the smallest operational contract required for deterministic Lead, Implementor and QA coordination over GitHub Issues and draft Pull Requests.

## Authoritative records

- The Issue body, Lead Dispatches and Lead Decisions define scope and authority.
- The current open PR Head identifies the Candidate.
- GitHub Actions evidence is valid only for that exact Head.
- Human-readable Issue comments remain the operational bus.
- Every dispatched worker publishes exactly one terminal `worker_result` envelope.

Raw GitHub snapshots are authoritative input. Derived state, attention and routing are reproducible read models and are not a second source of truth.

## Roles

### Lead

Lead dispatches work, resolves worker blockers, validates corrections and escalates to Owner only when an external decision is required.

### Implementor

Implementor delivers one coherent Candidate, leaves the PR draft for independent QA and reports one terminal result:

- `completed` — requested implementation is delivered;
- `no_op` — the dispatch was fully checked and no repository change is required;
- `blocked` — continuation requires Lead intervention.

### QA

QA reviews the exact Candidate Head and publishes one terminal result. A completed QA result includes `head` and one verdict:

- `approved`;
- `changes_required`;
- `inconclusive`.

## Mandatory terminal result

A dispatch never ends only with prose. The final comment contains free-form Markdown plus one live `supervisor:event` envelope conforming to [Supervisor Event Contract v1](supervisor-event-contract-v1.md). The envelope starts on its own line outside fenced examples and block quotes.

`no_op` is a complete outcome. It records what was inspected and advances to the next role; it does not repeat the same route merely because no commit was created.

## Lead-first blocker flow

```text
Implementor blocked ─┐
                     ├─→ Lead decision ─→ resume Implementor/QA
QA blocked ──────────┘                 └─→ Owner attention, only when owner_required
```

Implementor and QA never escalate directly to Owner. Their `blocked` result routes to Lead. Lead may:

- continue or correct with a validated `resume_role`, identifying the current blocker by stable GitHub comment ID in `resolves`;
- publish `owner_required` / `escalate_to: owner`, which creates Owner attention and no worker-chat dispatch.

A Lead result that does not correlate `resolves` to the active blocker cannot silently clear it.

## Exact-Head evidence

- Candidate-bound results use the full PR Head SHA.
- QA approval applies only to the current exact Head.
- A changed Head invalidates previous Implementor handoff and QA approval evidence.
- A fresh QA verdict for the current Head supersedes an older invalidated approval.
- CI summary affects routing only when its Head equals the current Candidate Head.
- Implementor completion/no-op waits for successful exact-Head CI before advancing.
- Stale evidence remains visible for audit but cannot advance the route.
- Multiple plausible open PRs are ambiguous and require Lead/manual attention; the Supervisor does not guess.

## Derived work-unit state

For every open Issue, Stage 3 derives:

- Project and repository identity;
- Issue/work-unit identity;
- lifecycle label or explicit `unknown`;
- selected Candidate or Candidate ambiguity;
- current Head and CI summary;
- parsed comments and latest Lead/Implementor/QA results;
- active blocker;
- QA reviewed and approved Head;
- warnings and last meaningful activity;
- attention classification;
- safe next route.

Missing lifecycle labels do not prevent analysis of comments, PRs or CI.

## Attention

The read model supports:

- `normal`;
- `waiting`;
- `action_required`;
- `ci_failed`;
- `blocked`;
- `owner_required`;
- `qa_invalidated`;
- `ambiguous`;
- `protocol_warning`;
- `terminal`.

Each attention item includes a stable machine-readable code and a human-readable explanation.

## Deterministic routing

Routing returns:

- action;
- target role, when a worker route exists;
- deterministic `lane_key` derived from Project, Issue and role;
- reason code and explanation;
- expected exact Head;
- guards and warnings.

Core transitions:

| Latest safe terminal result | Next route |
| --- | --- |
| Implementor `completed` / `no_op` with successful exact-Head CI | QA when required, otherwise Lead |
| Implementor `completed` / `no_op` with missing or pending exact-Head CI | wait |
| Implementor `blocked` | Lead |
| QA `approved` | Lead |
| QA `changes_required` | Implementor |
| QA `inconclusive` / `blocked` | Lead |
| Lead `continue` / `correct` | validated `resume_role` |
| Lead `owner_required` | Owner attention; no worker route |
| latest malformed, stale or ambiguous result | Lead/manual attention |

The Stage 3 router does not select browser profiles, tabs, chats or URLs.

## Future execution boundary

The planned execution chain is deliberately split:

1. OpenCode proposes an action and target role.
2. Policy Engine checks the proposal against workflow policy.
3. Lane Router resolves the deterministic role lane.
4. Browser Binding, implemented later, selects the concrete chat surface.

This separation keeps the current state engine independent from models and browser automation.
