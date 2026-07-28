$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($env:GITHUB_TOKEN)) {
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        throw 'GitHub CLI is required when GITHUB_TOKEN is not set. Install gh and run: gh auth login --git-protocol ssh'
    }
    & gh auth status | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'GitHub CLI is not authenticated. Run: gh auth login --git-protocol ssh'
    }
    $env:GITHUB_TOKEN = (& gh auth token).Trim()
    if ([string]::IsNullOrWhiteSpace($env:GITHUB_TOKEN)) {
        throw 'GitHub CLI returned an empty credential.'
    }
}

& docker compose up --build @args
exit $LASTEXITCODE
