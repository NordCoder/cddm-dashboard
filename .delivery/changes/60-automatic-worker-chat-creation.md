# Change — Automatically Create and Bind Fresh ChatGPT Worker Chats

Milestone: M8 — Worker Session Provisioning
Issue: #60
Risk: High
Authorized Base: `637a2a2fe25b755e6c5b244f4b535fe309201b6c`

## Outcome

Allow the local Dashboard and bundled Chrome extension to create a fresh ChatGPT conversation for the current routed role, optionally inside the exact ChatGPT Project mapped to the repository, initialize it with the role-specific Library resources, retain an exact persistent browser worker for that tab, and bind it to the current Work Unit lane without weakening Workflow Command authority.

## Requirements

- Provide an explicit **Create new chat** action for the current routed role.
- Provide durable Project mode `manual | automatic`.
- Provide durable optional `chatgpt_project_url` for the Dashboard Project/repository.
- Empty `chatgpt_project_url` creates a global ChatGPT conversation.
- Configured `chatgpt_project_url` opens that exact project page and requires the resulting conversation to remain inside its scope.
- While any Dashboard screen remains open, automatic mode supervises every enabled Project and creates only missing/unready Implementor and QA lanes from current backend `dispatch` routes.
- One managed ChatGPT tab has one persistent extension worker identity and exact-tab executor.
- Bootstrap references:
  - Lead: `@01-workflow.md`, `@cddm-minimal-issue-sizing-standard.md`;
  - Implementor: `@02-implementor-trigger.md`, `@gpt-gh-connector-guidelines.md`;
  - QA: `@03-qa-trigger.md`, `@gpt-gh-connector-guidelines.md`.
- Bootstrap contains no Workflow Command `command_id` and instructs the worker to wait.
- Existing current-route binding validation remains authoritative.
- Existing confirmed Browser Delivery sends the first real command.

## HARD HOW

- Use bounded Chrome external messaging only from enumerated loopback Dashboard origins.
- Use `tabs` only to create and revalidate exact ChatGPT tabs.
- Accept only credential-free `https://chatgpt.com/...` project pages without query or fragment; reject conversation URLs.
- Open the configured project page before bootstrap and prove the current creation surface matches its normalized project scope.
- Wait for a real conversation URL before registration or binding.
- For project-scoped creation, prove the final conversation URL belongs to the configured project or fail closed.
- Canonicalize global and project-scoped conversation URLs to the existing `/c/<conversation-id>` backend target identity.
- Include ChatGPT project scope in deterministic bootstrap request identity so changing the mapping cannot reuse an earlier completed request.
- Persist managed worker/tab identities, repository project scope and bootstrap request identities in extension storage.
- Never automatically replay an ambiguous or consumed bootstrap request.
- Keep primary manual target tracking separate from managed exact-tab workers.
- Register and poll every managed chat as an independent backend browser worker.
- The browser supervisor processes at most one new chat per poll cycle and relies on backend routes plus idempotent bootstrap identities.
- QA binding retirement remains version-specific and unchanged.
- Do not read or classify ChatGPT responses.
- Do not enable automatic merge.

## Out of Scope

- automatic Lead rotation;
- operation while the Dashboard browser is fully closed;
- response scraping;
- automatic plan generation;
- automatic merge;
- creating or editing ChatGPT Projects themselves;
- generalized browser automation;
- configurable bootstrap filenames;
- public or multi-user deployment.

## Verification

- SQLite migration and execution-profile URL validation tests;
- extension protocol, adapter, content, coordinator, runtime and manifest tests;
- frontend profile, bootstrap/routing and production build tests;
- existing backend and integration suites;
- exact-Head CI;
- disposable live smoke: repository Project URL → project bootstrap surface → scoped conversation URL → canonical `/c/<id>` target → managed presence → exact role binding → normal Workflow Command delivery.
