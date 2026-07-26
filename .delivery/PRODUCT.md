# CDDM Dashboard — Product Contract

## Purpose

CDDM Dashboard is a local/private control plane for supervising AI-assisted software delivery across multiple GitHub repositories.

The product reduces manual coordination by projecting canonical repository state into one operational workspace, deriving deterministic workflow state, preparing policy-controlled prompts, and delivering approved prompts to the intended AI work surface without granting the model hidden product authority.

## Primary User

A solo developer or small engineering team operating multiple repositories and AI-assisted delivery flows.

## Core Flow

```text
GitHub repository state
→ normalized project snapshots
→ deterministic workflow state and routing
→ bounded Prompt Context
→ OpenCode composition / deterministic fallback
→ Policy Engine
→ dashboard review and explicit user action
→ browser delivery to the intended ChatGPT conversation
```

## Current State

Stages 1–5 are complete:

- runnable Go/React/SQLite foundation;
- read-only multi-repository GitHub supervision;
- deterministic worker-result parsing, lifecycle, attention, and routing;
- OpenCode Prompt Planner with deterministic Policy Engine and fallback;
- responsive web dashboard with plan review, local editing, and manual Copy delivery.

The current product boundary ends at manual clipboard delivery.

## v1.0 Target

v1.0 adds:

- explicit browser/chat binding;
- confirmed Chrome-based prompt delivery without reading ChatGPT responses;
- stale, duplicate, restart, and offline-browser safety;
- mobile/private-network operation and pilot hardening.

## Product Boundaries

- GitHub remains the canonical external delivery-state source supervised by the product.
- Backend logic owns workflow lifecycle, routing, Candidate validity, blocker semantics, and policy decisions.
- OpenCode may compose wording but does not acquire routing or merge authority.
- ChatGPT Web responses are not read, scraped, classified, or persisted.
- Browser delivery is explicitly confirmed by the user by default.
- Automatic merge and unattended autonomous dispatch are outside the v1.0 default operating model.
- Credentials remain process/runtime configuration and are not stored in frontend state or model context.
- Initial deployment is local/private-network oriented, not public multi-tenant SaaS.
