# Builds the Kindle binary and assembles dist\extensions\kidsdash, ready to copy
# into the "extensions" folder on the Kindle.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

go run ./tools/fetchassets
if ($LASTEXITCODE -ne 0) { throw 'fetching assets failed' }

$ext = Join-Path $root 'dist\extensions\kidsdash'
if (Test-Path $ext) { Remove-Item -Recurse -Force $ext }
New-Item -ItemType Directory -Force (Join-Path $ext 'bin') | Out-Null
New-Item -ItemType Directory -Force (Join-Path $ext 'pictures') | Out-Null

# A static ARMv7 binary: no libc, so it runs on any Kindle firmware.
$env:GOOS = 'linux'; $env:GOARCH = 'arm'; $env:GOARM = '7'; $env:CGO_ENABLED = '0'
go build -trimpath -ldflags '-s -w' -o (Join-Path $ext 'bin\kidsdash') .
$built = $LASTEXITCODE
Remove-Item Env:GOOS, Env:GOARCH, Env:GOARM, Env:CGO_ENABLED
if ($built -ne 0) { throw 'build failed' }

Copy-Item kual\kidsdash\config.xml, kual\kidsdash\menu.json $ext
Copy-Item config.yaml, events.yaml $ext
if (Test-Path pictures) { Copy-Item pictures\* (Join-Path $ext 'pictures') -Recurse }

# Shell scripts must have Unix line endings or the Kindle refuses to run them.
foreach ($f in Get-ChildItem kual\kidsdash\bin\*.sh) {
    $text = [IO.File]::ReadAllText($f.FullName) -replace "`r`n", "`n"
    [IO.File]::WriteAllText((Join-Path $ext "bin\$($f.Name)"), $text, (New-Object Text.UTF8Encoding $false))
}

"Built $ext"
"Copy the folder dist\extensions\kidsdash into the 'extensions' folder on the Kindle."
