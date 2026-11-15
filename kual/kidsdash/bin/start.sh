#!/bin/sh
# Launched by KUAL.  Hands over to run.sh in the background so the KUAL menu
# can close while the dashboard keeps going.
DIR="$(cd "$(dirname "$0")/.." && pwd)"
(
	trap '' HUP
	exec sh "$DIR/bin/run.sh" "$@"
) >/dev/null 2>&1 &
