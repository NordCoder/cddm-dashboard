param(
    [string]$ApiOrigin = "http://localhost:1337",
    [Parameter(Mandatory = $true)][long]$ProjectId,
    [Parameter(Mandatory = $true)][int]$IssueNumber
)

$ErrorActionPreference = "Stop"
$uri = "$($ApiOrigin.TrimEnd('/'))/api/projects/$ProjectId/work-units/$IssueNumber/pilot-readiness"
$value = Invoke-RestMethod -Method Get -Uri $uri

Write-Host "Pilot Readiness: $($value.status)"
foreach ($check in $value.checks) {
    $state = if ($check.ready) { "READY" } else { "BLOCKED" }
    $detail = if ($check.detail) { " — $($check.detail)" } else { "" }
    Write-Host "[$state] $($check.code): $($check.status)$detail"
}
foreach ($warning in $value.protocol_warnings) {
    Write-Warning "$($warning.code): $($warning.message)"
}
if (-not $value.ready -or $value.status -ne "pilot_ready") {
    exit 1
}
