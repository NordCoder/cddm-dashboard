# CDDM Browser Delivery

This is the M6-C3 Manifest V3 executor. Load the `extension/` directory as an unpacked extension, open its options page, enter the configured backend origin (for example `http://localhost:8080`), and grant access only to that origin.

The service worker creates one durable installation `worker_id`, one fresh runtime `worker_session_id`, registers/heartbeats only the active supported ChatGPT conversation, and polls serially. A claimed command is reserved in `chrome.storage.local` before the content script is allowed to insert or send its exact prompt. Restart recovery marks an unresolved reservation `uncertain` and retries only its backend completion acknowledgement.

The content script has no response-message selectors or response persistence. It reads only the current URL and bounded composer/send-control state. It never scans other tabs, navigates, reads ChatGPT response text, accesses cookies, or uses clipboard/history/network interception permissions.

The broad-looking optional host-permission ceiling exists solely because Chrome requires optional host patterns to be declared before runtime requests. The extension requests and checks one exact configured origin at runtime; no backend host is enabled before that grant. ChatGPT access is the fixed `https://chatgpt.com/*` surface.

Focused verification:

```sh
npm test
node --check src/service-worker.js
```

Practical browser verification should use a disposable supported ChatGPT conversation and confirm: registration/heartbeat target transitions, one successful exact prompt send, duplicate claim/restart no-resend behavior, navigation-away fail-closed behavior, and offline acknowledgement recovery. The extension never uses ChatGPT response content as an oracle.
