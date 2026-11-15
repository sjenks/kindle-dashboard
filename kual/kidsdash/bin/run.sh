#!/bin/sh
# Takes over the screen, runs the dashboard, and gives the Kindle back when
# the dashboard exits (unlock sequence, stop.sh, or a crash).
DIR="$(cd "$(dirname "$0")/.." && pwd)"
PIDFILE=/var/tmp/kidsdash.pid
LOG="$DIR/kidsdash.log"

# Only one at a time.
if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
	exit 0
fi
echo $$ > "$PIDFILE"

[ -f "$LOG" ] && mv -f "$LOG" "$LOG.old"
exec >> "$LOG" 2>&1
echo "$(date) starting ($*)"

# Give KUAL a moment to close its menu.
sleep 3

# Keep the Kindle awake, hide its status bar, and freeze its window manager so
# the normal interface neither redraws over the dashboard nor reacts to input.
lipc-set-prop com.lab126.powerd preventScreenSaver 1
lipc-set-prop com.lab126.pillow disableEnablePillow disable
killall -STOP awesome

if [ "$1" = "keytest" ]; then
	"$DIR/bin/kidsdash" -config "$DIR/config.yaml" -keytest
else
	"$DIR/bin/kidsdash" -config "$DIR/config.yaml"
fi
echo "$(date) dashboard exited with status $?"

# Hand the Kindle back.
killall -CONT awesome
lipc-set-prop com.lab126.pillow disableEnablePillow enable
lipc-set-prop com.lab126.powerd preventScreenSaver 0
# Make the home screen repaint over the last dashboard frame.
lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.home

rm -f "$PIDFILE"
