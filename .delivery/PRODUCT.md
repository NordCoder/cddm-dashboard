# CDDM Dashboard — Product Contract

## Purpose

CDDM Dashboard is a local/private control plane for supervising AI-assisted software delivery across multiple GitHub repositories.

It projects canonical GitHub state into one operational workspace, derives deterministic workflow routes, creates policy-controlled Workflow Commands, delivers versioned role prompts to exact ChatGPT conversations, and accepts terminal worker claims only through validated GitHub Issue comments.

## Primary user

A solo developer or small engineering team operating multiple repositories and AI-assisted Lead, Implementor, and QA flows.

## Core flow

```text
GitHub facts
→ normalized Project / Work Unit snapshot
→ deterministic route and Prompt Context
→ policy-approved Workflow Command
→ exact Browser Delivery Command
→ bound ChatGPT role conversation
→ worker GitHub Issue comment
→ cddm-worker-result/v1 validation and correlation
→ external GitHub fact verification
→ next deterministic route
```

## Implemented

- Go/React/SQLite local-first application;
- read-only multi-repository GitHub synchronization;
- deterministic lifecycle, Candidate, blocker, CI, QA freshness, attention, and routing;
- Prompt Context, Prompt Plan, Policy Engine, OpenCode composition, and deterministic fallback;
- responsive Supervisor workspace and mobile layout;
- browser worker identity, exact lane-to-chat binding, Chrome MV3 delivery, reviewed delivery, opt-in auto-send, Manual Copy fallback, durable claims, and uncertain-delivery safety;
- repository-owned `cddm-dashboard-resources/v1.0` role resources and `cddm-worker-result/v1` schema;
- durable Workflow Commands and Worker Results, marker validation/correlation, conflict handling, GitHub readback verification, and deterministic next routing;
- typed Work Unit execution surfaces, Lead/Implementor/QA bindings, `manual_fresh_binding` QA mode, and Project Pilot Readiness diagnostics;
- restart, duplicate synchronization, delivered-without-result, downtime-result, exact-Head, and combined integration fixtures.

## Pilot-ready boundary

The product is ready for a controlled local/private pilot after the operator:

1. installs and starts the current build;
2. configures GitHub authentication and browser delivery;
3. adds a Project;
4. binds Lead and Implementor conversations;
5. uses a fresh QA binding when requested;
6. obtains a successful Pilot Readiness result.

Pilot-ready does not mean a pilot was executed. `NordCoder/misak-website#140` remains untouched by this integration.

## Product boundaries

- GitHub facts are the external authority for PR identity, exact Head, CI, QA freshness, mergeability, and merge result.
- Browser Delivery, Workflow Command, Worker Result, and GitHub facts are distinct records.
- A worker-result marker is a claim, never authority by itself.
- Backend code owns routing, Candidate validity, blocker semantics, correlation, verification, and policy decisions.
- ChatGPT response content is never read, scraped, classified, or persisted.
- Auto-send may deliver an already authorized command; it does not create product authority.
- `auto_merge=false`; merge remains explicit Lead/Owner authority.
- Credentials remain process/runtime configuration and are not stored in model context, frontend state, or Project records.
- Initial deployment is loopback/local-private, not public multi-tenant SaaS.

## Future automation

Possible later work includes authenticated remote deployment, automatic creation of fresh ChatGPT conversations, richer notification channels, and explicitly approved merge automation. None is required for the current pilot.
