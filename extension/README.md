# CDDM Browser Delivery

This is the M6-C3 Manifest V3 executor. Load the `extension/` directory as an unpacked extension, verify the stable ID `biakfbpkfdpniphmoafgldedkbnjfibp`, open its options page, enter the configured loopback backend origin (for example `http://localhost:8080`), and grant access only to that origin. The manifest public key fixes the unpacked extension ID; it is not a credential.

The service worker creates one durable installation `worker_id`, one fresh runtime `worker_session_id`, registers/heartbeats only the explicitly activated supported ChatGPT conversation, and polls serially. A claimed command is validated against its SHA-256 prompt hash and reserved in a serialized, backend-origin-bound `chrome.storage.local` ledger before the content script may insert or send its exact prompt. Restart recovery marks unresolved reservations `uncertain` and retries only same-origin backend completion acknowledgement. A concurrent duplicate that observes `reserved` cannot send or complete the in-progress claim.

The content script has no response-message selectors or response persistence. It reads only the exact conversation URL and bounded identified composer/send-control state. It never scans other tabs, navigates, reads ChatGPT response text, accesses cookies, or uses clipboard/history/network interception permissions. A Send click is reported `delivered` only after the composer clears as bounded submit acknowledgement; otherwise the outcome is `uncertain`.

The broad-looking optional host-permission ceiling exists solely because Chrome requires optional host patterns to be declared before runtime requests. The extension requests and checks one exact configured origin at runtime; no backend host is enabled before that grant. Changing origins revokes the previous permission, and unfinished claims are never acknowledged across backend origins. ChatGPT access is the fixed `https://chatgpt.com/*` surface. The backend accepts extension API requests only from this manifest's stable extension ID.

Focused verification:

```sh
npm test
node --check src/service-worker.js
```

Practical browser verification should use a disposable supported ChatGPT conversation and confirm: registration/heartbeat target transitions, one successful exact prompt send, duplicate claim/restart no-resend behavior, navigation-away fail-closed behavior, backend-origin replacement isolation, and offline acknowledgement recovery. The extension never uses ChatGPT response content as an oracle.
