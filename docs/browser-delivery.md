# Confirmed browser delivery

CDDM Dashboard can provision exact ChatGPT worker conversations and deliver one current backend-authorized Workflow Command to one explicitly bound conversation through the bundled Chrome Manifest V3 extension. The browser path never reads ChatGPT responses.

Browser delivery is transport only:

```text
Chat bootstrap = role-session initialization
Browser Delivery status = prompt transport evidence
Workflow Command status = assignment execution state
Worker Result = terminal GitHub comment evidence
```

A bootstrap message contains no Workflow Command authority. A `delivered` browser command becomes `awaiting_result`; it does not complete the Workflow Command.

## Network boundary

The dashboard and browser-delivery APIs do not provide a public authentication layer. Docker Compose publishes both host ports on `127.0.0.1` by default, and the backend rejects non-loopback HTTP Host authorities for reads and mutations.

`BIND_HOST` changes socket publication only; it does not relax the backend Host guard. Use `localhost`, `127.0.0.1`, or `::1`. Do not expose the API or dashboard directly to an untrusted LAN or the public internet.

State-changing browser requests must be same-origin and JSON bodies must use `application/json`. `chrome-extension://` access is accepted only from bundled extension ID `biakfbpkfdpniphmoafgldedkbnjfibp` and only on `/api/browser/`. Dashboard-to-extension chat provisioning is accepted only from the enumerated local Dashboard origins in the extension manifest.

## Enable the backend

Browser delivery is opt-in. In `.env` set:

```env
BROWSER_DELIVERY_ENABLED=true
BROWSER_BINDING_TTL=30s
BROWSER_DELIVERY_PENDING_TTL=5m
BROWSER_DELIVERY_CLAIM_TTL=1m
```

Start or restart:

```bash
bash scripts/cddm-up.sh -d
```

- Dashboard: `http://localhost:1338`
- API: `http://localhost:1337`

## Load the extension

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Select **Load unpacked** and choose `extension/`.
4. Confirm ID `biakfbpkfdpniphmoafgldedkbnjfibp`.
5. Open extension **Options**.
6. Set the backend origin to `http://localhost:1338` or `http://localhost:1337` and grant only that origin.
7. Reload the extension after upgrades so its current bounded permissions are applied.

The manifest public key fixes the unpacked extension identity; it is not a credential. Changing the configured backend revokes the previous origin permission. Each durable claim is bound to the normalized backend origin that issued it.

The extension uses the `tabs` permission only to create and revalidate exact ChatGPT tabs. It does not request history, cookies, webRequest, clipboard-read or broad page origins.

## Role conversations

A Work Unit exposes independent logical bindings:

```text
<owner>/<repository>#<issue>:lead
<owner>/<repository>#<issue>:implementor
<owner>/<repository>#<issue>:qa
```

### Manual binding

1. Open the intended ChatGPT conversation and activate that tab once.
2. Open the current Work Unit.
3. Select the live target for the required role.
4. Choose **Bind** or **Rebind**.

The UI never accepts a free-text conversation URL for binding. The extension remembers the exact activated tab for the primary manual browser worker and revalidates the same tab ID and canonical `/c/<conversation-id>` target.

### Repository ChatGPT Project mapping

Each Dashboard Project execution profile has:

```json
{
  "chat_creation_mode": "manual",
  "chatgpt_project_url": ""
}
```

On the Dashboard Project page, paste the exact ChatGPT Project page that should own chats for that repository. The backend accepts only a credential-free `https://chatgpt.com/...` URL without query or fragment and rejects conversation URLs. A trailing slash is removed before persistence.

- empty `chatgpt_project_url` — create a global ChatGPT conversation;
- configured `chatgpt_project_url` — open that exact project page and create the conversation inside its scope.

This is a creation scope, not part of the durable browser target identity. The final target remains canonical:

```text
https://chatgpt.com/c/<id>               → /c/<id>
https://chatgpt.com/<project>/c/<id>     → /c/<id>
```

The extension separately proves that a project-scoped final URL belongs to the configured project before registering or binding the canonical target.

### Dashboard-created chat

When the current route has `action=dispatch`, choose **Create new chat** for the routed role. The Dashboard sends one bounded external request to the extension containing:

- Project, Issue, role and expected logical lane;
- current binding version when replacing a lane;
- the durable `chatgpt_project_url`, when configured;
- one role bootstrap message;
- an idempotent bootstrap request identity derived from lane, binding generation and ChatGPT project scope.

The extension then:

1. creates a new active tab at global ChatGPT or the exact configured ChatGPT Project page;
2. sends the role bootstrap only on the expected empty creation surface;
3. waits for the resulting conversation URL;
4. verifies that a project-scoped conversation belongs to the configured project;
5. canonicalizes the target as `/c/<conversation-id>`;
6. creates a persistent browser `worker_id` dedicated to that exact tab;
7. registers fresh backend presence for that worker/target;
8. calls the existing current-route browser-binding endpoint;
9. returns the verified binding evidence to the Dashboard.

A bootstrap on the wrong project page fails with `bootstrap_project_surface_invalid`. A resulting conversation outside the configured project fails with `created_chat_outside_configured_project`. Neither result is automatically rebound or retried as another bootstrap because a chat may already have been created.

Every managed chat keeps its own worker identity, session identity and exact tab adapter. Activating another ChatGPT tab does not replace or invalidate it. Closing or navigating its exact tab makes only that managed binding unavailable.

A completed bootstrap request is idempotent. An ambiguous or failed consumed request is not replayed automatically because a conversation may already have been created. The operator can inspect the opened tabs and manually bind a proven target instead of risking a duplicate.

## Automatic Implementor and QA creation

The Project and Work Unit UI provide a durable Project-scoped preference:

- **Manual** — chat creation and binding remain explicit;
- **Auto-create Implementor + QA** — while any Dashboard screen remains open, a global browser supervisor scans every enabled Dashboard Project and provisions one current routed lane per poll cycle when its binding is missing or not ready.

Automatic mode never creates Lead authority or rotates the Lead chat. It reacts only to backend-derived `dispatch` routes:

- a new Issue routed to Implementor receives a new Implementor chat;
- after an accepted Implementor result routes to QA, a fresh QA chat is created and bound;
- after terminal QA, the command-bound QA binding is retired as before;
- a later QA cycle receives another fresh chat because the retired binding version is no longer ready.

For every one of those operations, the supervisor reads the current `chatgpt_project_url` from the same durable execution profile. Changing that URL changes the bootstrap request identity; a completed operation from a previous ChatGPT Project cannot be reused for the new scope.

The bootstrap resources are fixed in this Change:

```text
Lead:        @01-workflow.md + @cddm-minimal-issue-sizing-standard.md
Implementor: @02-implementor-trigger.md + @gpt-gh-connector-guidelines.md
QA:          @03-qa-trigger.md + @gpt-gh-connector-guidelines.md
```

The bootstrap explicitly tells the worker to wait. It contains no `command_id`, does not modify GitHub and cannot complete a workflow action. The next real command uses the normal Planner → Workflow Command → confirmed Browser Delivery path.

QA retains `manual_fresh_binding` semantics at the backend level: every accepted terminal QA result retires exactly the binding/version captured by its delivery command. Automatic creation is transport assistance, not a relaxation of QA independence.

The supervisor stops when the Dashboard browser is fully closed. It does not introduce a server-side background dispatcher or new GitHub authority.

## Reviewed delivery

Reviewed mode is the default.

1. Inspect the current route, role, Candidate, exact Head, resource version, binding version and immutable prompt.
2. Confirm the bound ChatGPT target.
3. Choose **Confirm and send**.

One confirmation intent receives one idempotency key. Network failure, unreadable success data or HTTP `5xx` retains the same frozen intent because the backend may already have committed it. A definitive stale/conflicting response cancels the intent. `uncertain` DOM outcomes are never automatically replayed.

## Auto-send

Auto-send is an opt-in delivery preference stored per Project/Work Unit. It may deliver an already created, fully checked current Workflow Command when the exact binding is ready.

It does not:

- create material Owner authority;
- bypass the current route, expected Head, context hash or resource version;
- create another Workflow Command from the same accepted result;
- replay an uncertain DOM send;
- enable automatic merge.

The Project execution profile keeps `delivery_mode = reviewed | auto` separate from `auto_merge = false`.

Historical `/plans/:planID` screens never perform current workflow actions. Manual Copy remains available for edited text or unavailable browser transport.

## At-most-once and recovery behavior

- backend confirmation freezes plan, Head, lane, binding and presence identities;
- one Workflow Command links to at most one Browser Delivery Command;
- every managed chat is addressed by its persistent exact tab ID;
- project-scoped bootstrap identity includes the configured ChatGPT Project URL;
- the extension durably reserves a claim before inserting or sending text;
- exact target and backend checks run before insertion and again before send;
- the prompt SHA-256 digest is verified before DOM access;
- one claim has one executor owner;
- a Send click is `delivered` only after bounded composer-clear acknowledgement;
- unchanged or unrelatedly edited composer state becomes `uncertain`;
- restart recovery never replays a completed or uncertain DOM send;
- ordinary completion transport retry may repeat only acknowledgement for the same claim;
- browser requests are time-bounded;
- the extension never reads, classifies or persists response content.

## Worker completion

The extension does not inspect the ChatGPT response. The worker publishes one GitHub Issue comment containing at most one `cddm-worker-result/v1` marker with the originating `command_id`. Dashboard synchronization validates and correlates that marker, verifies consequential GitHub facts, and derives the next route.

See [Dashboard worker loop](worker-loop.md) and [Controlled pilot guide](pilot-guide.md).
