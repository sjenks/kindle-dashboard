# Dot-source this to use the project-local Go toolchain:  . .\scripts\env.ps1
# Everything (toolchain, module cache, build cache) stays inside .tools\.
$root = Split-Path -Parent $PSScriptRoot
$env:GOROOT = Join-Path $root '.tools\go'
$env:GOPATH = Join-Path $root '.tools\gopath'
$env:GOCACHE = Join-Path $root '.tools\gocache'
$env:GOTOOLCHAIN = 'local'
$env:GOFLAGS = '-modcacherw'
$env:Path = (Join-Path $env:GOROOT 'bin') + ';' + $env:Path
