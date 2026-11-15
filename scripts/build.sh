#!/bin/sh
# Builds the Kindle binary and assembles dist/extensions/kidsdash, ready to copy
# into the "extensions" folder on the Kindle.  macOS/Linux twin of build.ps1.
#   scripts/build.sh            build only
#   scripts/build.sh --install  build, then copy onto a Kindle mounted at /Volumes/Kindle
set -e
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
. scripts/env.sh

go run ./tools/fetchassets

ext="$root/dist/extensions/kidsdash"
rm -rf "$ext"
mkdir -p "$ext/bin" "$ext/pictures"

# A static ARMv7 binary: no libc, so it runs on any Kindle firmware.
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
	go build -trimpath -ldflags '-s -w' -o "$ext/bin/kidsdash" .

cp kual/kidsdash/config.xml kual/kidsdash/menu.json "$ext/"
cp config.yaml events.yaml "$ext/"
# Private calendar links: never in git (see .gitignore), only on the Kindle.
[ -f calendars.txt ] && cp calendars.txt "$ext/"
[ -d pictures ] && cp -R pictures/. "$ext/pictures/"

# Shell scripts must have Unix line endings or the Kindle refuses to run them.
for f in kual/kidsdash/bin/*.sh; do
	tr -d '\r' < "$f" > "$ext/bin/$(basename "$f")"
done
chmod +x "$ext/bin/"*

echo "Built $ext"

if [ "$1" = "--install" ]; then
	kindle=/Volumes/Kindle
	[ -d "$kindle/extensions" ] || { echo "No Kindle with an extensions folder at $kindle" >&2; exit 1; }
	# Keep the Kindle's log between installs, and its calendars.txt if there is
	# none here; replace everything else.
	rm -rf "$kindle/extensions/kidsdash.new"
	cp -R "$ext" "$kindle/extensions/kidsdash.new"
	keep_files="kidsdash.log kidsdash.log.old"
	[ -f calendars.txt ] || keep_files="$keep_files calendars.txt"
	for keep in $keep_files; do
		[ -f "$kindle/extensions/kidsdash/$keep" ] && cp "$kindle/extensions/kidsdash/$keep" "$kindle/extensions/kidsdash.new/"
	done
	rm -rf "$kindle/extensions/kidsdash"
	mv "$kindle/extensions/kidsdash.new" "$kindle/extensions/kidsdash"
	# Drop the ._* metadata files macOS writes on FAT volumes.
	dot_clean -m "$kindle/extensions/kidsdash" 2>/dev/null || true
	sync
	echo "Installed to $kindle/extensions/kidsdash. Eject the Kindle, then KUAL > Kids Dashboard."
fi
