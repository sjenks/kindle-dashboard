# Source this to use the project-local Go toolchain:  . scripts/env.sh
# Everything (toolchain, module cache, build cache) stays inside .tools/.
_root="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
export GOROOT="$_root/.tools/go"
export GOPATH="$_root/.tools/gopath"
export GOCACHE="$_root/.tools/gocache"
export GOTOOLCHAIN=local
export GOFLAGS=-modcacherw
export PATH="$GOROOT/bin:$PATH"
unset _root
