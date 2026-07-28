# Installation

## Prerequisites

- Docker with Compose;
- GitHub CLI for the recommended credential flow;
- Chrome or Chromium with unpacked-extension support;
- Bash on Linux/macOS/WSL or PowerShell on Windows.

## Install and start

```bash
gh auth login --git-protocol ssh
cp .env.example .env
```

Set browser delivery in `.env`:

```env
GITHUB_AUTH_MODE=auto
BROWSER_DELIVERY_ENABLED=true
```

Start:

```bash
bash scripts/cddm-up.sh -d
```

PowerShell:

```powershell
./scripts/cddm-up.ps1 -d
```

Open:

- Dashboard: `http://localhost:1338`
- API: `http://localhost:1337`

Both host ports remain loopback-bound. Do not expose this build directly to an untrusted LAN or the public internet.

## Load the extension

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Choose **Load unpacked** and select `extension/`.
4. Confirm extension ID `biakfbpkfdpniphmoafgldedkbnjfibp`.
5. Open extension Options and set the backend origin to `http://localhost:1338` or `http://localhost:1337`.
6. Grant only the requested origin permission.

## Verify packaged resources

The server validates and embeds:

```text
resources/cddm-dashboard-resources/v1.0/
```

Startup fails if its manifest, role prompts, result marker instructions, schema, version, or digest are invalid.

## Database

SQLite is migrated automatically through schema version 9. Docker stores it in the `cddm_data` volume. Back up the volume before destructive local maintenance.

## Upgrade

1. stop the current containers;
2. update the repository to the accepted main revision;
3. rerun `bash scripts/cddm-up.sh -d` or the PowerShell launcher;
4. confirm healthy Project synchronization and run the Pilot Readiness check.

No credential is written to `.env` by the launchers.
