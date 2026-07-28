# Confirmed browser delivery

CDDM Dashboard can deliver one current backend-approved Prompt Plan to one explicitly bound ChatGPT conversation through the bundled Chrome Manifest V3 extension. The browser path never reads ChatGPT responses.

## Network boundary

The dashboard and browser-delivery APIs do not provide a public authentication layer. Docker Compose therefore publishes both host ports on `127.0.0.1` by default, and the backend rejects non-loopback HTTP Host authorities for reads and mutations. This blocks direct LAN exposure and DNS-rebinding access to local Project, Prompt Plan, binding, and delivery data.

`BIND_HOST` changes only socket publication; it does not relax the backend Host guard. This build is intended to be opened as `localhost`, `127.0.0.1`, or `::1`. Do not expose the API or dashboard directly to an untrusted LAN or the public internet.

State-changing browser requests must also be same-origin, and JSON bodies must use `application/json`. `chrome-extension://` access is accepted only from the stable bundled extension ID `biakfbpkfdpniphmoafgldedkbnjfibp` and only on `/api/browser/`.

## Enable the backend

Browser delivery is deliberately opt-in. In `.env` set:

```env
BROWSER_DELIVERY_ENABLED=true
BROWSER_BINDING_TTL=30s
BROWSER_DELIVERY_PENDING_TTL=5m
BROWSER_DELIVERY_CLAIM_TTL=1m
```

Then start or restart the application:

```bash
bash scripts/cddm-up.sh -d
```

The normal Compose web endpoint is `http://localhost:1338`; it proxies `/api` to the backend. Direct backend access remains available at `http://localhost:1337`.

## Load the extension

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Select **Load unpacked** and choose the repository `extension/` directory.
4. Confirm that the displayed extension ID is `biakfbpkfdpniphmoafgldedkbnjfibp`.
5. Open the extension **Options** page.
6. Set the backend origin to either the proxied app origin (`http://localhost:1338`) or direct API origin (`http://localhost:1337`). Grant only the requested origin permission.

The manifest public key fixes the unpacked extension identity; it is not a credential. Changing the configured backend revokes the previously granted backend origin. Each durable claim is bound to the exact normalized backend origin that issued it.

## Bind a conversation

1. Open the intended `https://chatgpt.com/c/...` conversation and make that tab active once.
2. Open the relevant CDDM work unit or latest Prompt Plan in the dashboard.
3. The **Browser Delivery** panel lists only live browser targets projected by the backend. It does not accept a free-text ChatGPT URL.
4. Select the target and choose **Bind target** or **Rebind target**.

After a ChatGPT tab has been explicitly activated, the extension remembers that exact tab for the current browser session. Switching to the dashboard does not lose the target: the extension revalidates the same tab ID and URL. Closing the tab, navigating it away, browser restart, conflicting extension sessions, or presence timeout makes the binding unavailable until freshness is proved again.

## Manual confirmation mode

Browser delivery is enabled only when the backend still projects a dispatchable current plan and the binding is `ready`.

1. Choose **Review delivery**.
2. Verify the plan, exact Head, lane, binding version, ChatGPT target and exact immutable backend prompt.
3. Choose **Confirm and send**.

One confirmation intent receives one idempotency key. Network failure, unreadable or malformed success data, or HTTP `5xx` keeps that same frozen intent because the backend may already have committed it. A definitive stale/conflicting response cancels the intent and requires another review. `uncertain` delivery outcomes are never automatically replayed at the DOM layer.

## Automatic mode

The Browser Delivery header contains an **Auto-send** switch. It is disabled by default and stored in local browser storage.

When enabled, the dashboard automatically creates one delivery command for each new exact approved Prompt Plan as soon as the current binding is `ready`. The mode skips the review screen but does not skip backend authority validation.

Automatic mode:

- uses only the immutable backend-generated prompt;
- pauses when the visible prompt textarea has local edits;
- derives its identity from project, issue, plan, plan hash, context hash, exact Head, lane, binding version and presence token;
- stores one stable idempotency key for that exact identity;
- detects a manually created command for the same exact plan and binding and does not create another;
- throttles transport-ambiguous retries and reuses the same key;
- blocks repeated attempts after a definitive backend rejection until the plan or binding identity changes.

Automatic mode requires the relevant Work Unit or Prompt Plan page to remain open in the dashboard. It is a browser-local operator preference, not a server-wide scheduler.

Local edits in the prompt textarea are intentionally not sent by either Browser Delivery mode. Use **Manual Copy** for edited prompt text or reset to the backend-generated prompt first.

## Safety behavior

- the backend remains authoritative for current plan, Head, lane, binding and command lifecycle;
- confirmation requests carry only immutable/CAS identities, never replacement prompt or target authority;
- the extension validates opaque command/claim/session identities and verifies the claimed prompt against the backend SHA-256 hash before touching the DOM;
- the extension durably records a backend-origin-bound claim before DOM send and serializes ledger updates;
- an in-progress reserved claim has one executor owner and cannot be completed or sent by a concurrent duplicate;
- target identity and backend configuration are checked before insertion and again before send;
- DOM matching is limited to identified ChatGPT composer/send controls;
- a Send click is `delivered` only after the composer clears as bounded submit acknowledgement; an unchanged or unrelatedly edited composer is `uncertain`;
- definitive completion rejection statuses become bounded terminal diagnostics and do not cause a DOM resend;
- ordinary completion transport failures may retry only the acknowledgement for the same origin, never the DOM send;
- backend requests are time-bounded;
- the extension does not read, scrape, classify or persist ChatGPT response content;
- Manual Copy remains available independently of browser availability.
