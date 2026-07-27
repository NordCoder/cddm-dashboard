# Confirmed browser delivery

CDDM Dashboard can deliver one current backend-approved Prompt Plan to one explicitly bound ChatGPT conversation through the bundled Chrome Manifest V3 extension. The browser path never reads ChatGPT responses.

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
docker compose up --build
```

The normal Compose web endpoint is `http://localhost:3000`; it proxies `/api` to the backend. Direct backend access remains available at `http://localhost:8080`.

## Load the extension

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Select **Load unpacked** and choose the repository `extension/` directory.
4. Open the extension **Options** page.
5. Set the backend origin to either the proxied app origin (`http://localhost:3000`) or direct API origin (`http://localhost:8080`). Grant only the requested origin permission.

Changing the configured backend revokes the previously granted backend origin. The extension never stores backend credentials.

## Bind a conversation

1. Open the intended `https://chatgpt.com/c/...` conversation and make that tab active once.
2. Open the relevant CDDM work unit or latest Prompt Plan in the dashboard.
3. The **Browser Delivery** panel lists only live browser targets projected by the backend. It does not accept a free-text ChatGPT URL.
4. Select the target and choose **Bind selected target** (or **Rebind selected target**).

After a ChatGPT tab has been explicitly activated, the extension remembers that exact tab for the current browser session. Switching to the dashboard does not lose the target: the extension revalidates the same tab ID and URL. Closing the tab, navigating it away from the bound conversation, browser restart, conflicting extension sessions, or presence timeout makes the binding unavailable until freshness is proved again.

## Confirm one delivery

Browser delivery is enabled only when the backend still projects a dispatchable current plan and the binding is `ready`.

1. Choose **Review delivery**.
2. Verify the plan, exact Head, lane, binding version, ChatGPT target and exact immutable backend prompt.
3. Choose **Confirm and send**.

One confirmation intent receives one idempotency key. A transport-ambiguous retry reuses the frozen confirmation and the same key. A stale/conflicting response cancels that intent and requires another review. `uncertain` delivery outcomes are never automatically retried.

Local edits in the Stage 5 prompt textarea are intentionally not sent by browser delivery; use **Manual Copy** for edited prompt text or reset to the backend-generated prompt first.

## Safety behavior

- the backend remains authoritative for current plan, Head, lane, binding and command lifecycle;
- the dashboard confirmation request carries only immutable/CAS identities, never replacement prompt or target authority;
- the extension durably records a claim before DOM send and never sends the same claim twice;
- target identity is checked before insertion and again before clicking Send;
- completion `409 Conflict` is terminal diagnostic state locally and does not cause a DOM resend;
- ordinary completion transport failures may retry the acknowledgement but never the DOM send;
- the extension does not read, scrape, classify or persist ChatGPT response content;
- Manual Copy remains available independently of browser availability.
