# Renders the dashboard to preview.png on this PC, using config.yaml and
# events.yaml from the project folder.  Extra arguments are passed through:
#   .\scripts\preview.ps1 -now 2026-12-24T08:30
. (Join-Path $PSScriptRoot 'env.ps1')
Set-Location (Split-Path -Parent $PSScriptRoot)
go run . -preview preview.png @args
