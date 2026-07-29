# Confirmed browser delivery

CDDM Dashboard can deliver one current backend-authorized Workflow Command to one explicitly bound ChatGPT conversation through the bundled Chrome Manifest V3 extension. The browser path never reads ChatGPT responses.

Browser delivery is transport only:

```text
Browser Delivery status = prompt transport evidence
Workflow Command status = assignment execution state
Worker Result = terminal GitHub comment evidence
```

A `delivered` browser command becomes `awaiting_result`; it does not complete the Workflow Command.

## Network boundary

The dashboard and browser-delivery APIs do not provide a public authentication layer. Docker Compose publishes both host ports on `127.0.0.1` by default, and the backend rejects non-loopback HTTP Host authorities for reads and mutations.

`BIND_HOST` changes socket publication only; it does not relax the backend Host guard. Use `localhost`, `127.0.0.1`, or `::1`. Do not expose the API or dashboard directly to an untrusted LAN or the public internet.

State-changing browser requests must be same-origin and JSON bodies must use `application/json`. `chrome-extension://` access is accepted only from bundled extension ID `biakfbpkfdpniphmoafgldedkbnjfibp` and only on `/api/browser/`.

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

The manifest public key fixes the unpacked extension identity; it is not a credential. Changing the configured backend revokes the previous origin permission. Each durable claim is bound to the normalized backend origin that issued it.

## Bind role conversations

A Work Unit exposes independent logical bindings:

```text
<owner>/<repository>#<issue>:lead
<owner>/<repository>#<issue>:implementor
<owner>/<repository>#<issue>:qa
```

1. Open the intended `https://chatgpt.com/c/...` conversation and activate that tab once.
2. Open the current Work Unit.
3. Select the live target for the required role.
4. Choose **Bind** or **Rebind**.

The UI never accepts a free-text ChatGPT conversation URL. The extension remembers the exact activated tab for the browser session and revalidates the same tab ID and URL. Closing, navigating, restarting, conflicting extension sessions, or presence timeout makes the binding unavailable until freshness is proved again.

QA uses `manual_fresh_binding`. Keep QA unbound until the current route requires fresh QA, then open and bind a new QA conversation. After an accepted terminal QA result, the exact binding/version captured by the delivery command is retired. A newer replacement version is not retired.

## Automatic role-session provisioning

M11 adds a separate provisioning path for the continuous profile. It does not replace the existing manual binding flow and it does not create or deliver a normal Workflow Command.

Configure the exact ChatGPT Project page in the Project autonomy profile when repository-scoped chats are required. An empty `chatgpt_project_url` creates a global ChatGPT conversation. A configured value must be an exact credential-free `https://chatgpt.com/...` Project page URL without query, fragment or conversation suffix.

The durable lifecycle is:

```text
active M10 lane lease
→ pending provisioning request
→ extension claim
→ exact inactive ChatGPT creation tab
→ surface_ready
→ exact ordered Library attachments
→ bootstrap-only send
→ exact global/Project conversation URL
→ live managed worker registration
→ atomic current-Head validation + role binding + provisioned
```

The fixed `cddm-dashboard-resources/v2.0` bootstrap profiles are:

```text
Lead:        01-lead-trigger.md + gpt-gh-connector-guidelines.md
Implementor: 02-implementor-trigger.md + gpt-gh-connector-guidelines.md
QA:          03-qa-trigger.md + gpt-gh-connector-guidelines.md
```

Each Library filename is NFC-normalized and matched as a complete filename. Missing, partial or duplicate matches fail before send. After every selection, the content script verifies the exact selected attachment chip and finally verifies ordered chip evidence against the frozen backend profile.

Bootstrap contains initialization only and instructs the worker to wait for a normal Workflow Command. It is not assignment authority.

The extension persists these bootstrap phases:

```text
not_started → send_reserved → sent → target_observed → provisioned
```

`send_reserved` is written before the click. A service-worker restart in that phase becomes terminal `uncertain`; it never replays the bootstrap. After `sent`, restart recovery may only observe the conversation URL, re-register the exact target and retry backend finalization.

Session policy:

- a healthy Project Lead conversation may be reused without another bootstrap;
- every Implementor Intent receives a fresh conversation;
- every QA Intent receives a fresh conversation and remains exact-Head bound;
- successful fresh-role rotation supersedes prior same-scope provisioning evidence and disables its old binding atomically;
- tab closure, Project-scope drift, stale Head, changed lease or binding-version conflict fails closed.

The backend finalization transaction revalidates the current provisioning request, active lease, claimed Intent, synchronized Issue/PR, exact Candidate Head, Project URL, live worker target, attachment evidence and expected binding version. Only that transaction may set `status=provisioned`.

Operator reads expose queue status, completion reason, attachment evidence, observed ChatGPT URL and bound binding ID/version. No ChatGPT response content is stored.

The ChatGPT DOM and Library UI are external surfaces. Repository tests prove the fail-closed matching and restart behavior, but a disposable live smoke is still required after ChatGPT UI changes or before integrating the stacked M11 chain.

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

The extension does not inspect the ChatGPT response. The worker publishes one GitHub Issue comment containing at most one version-compatible worker-result marker with the originating `command_id`. Dashboard synchronization validates and correlates that marker, verifies consequential GitHub facts, and derives the next route.

See [Dashboard worker loop](worker-loop.md) and [Controlled pilot guide](pilot-guide.md).
