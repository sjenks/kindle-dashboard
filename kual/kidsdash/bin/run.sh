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

# Weather and calendars need the network: switch Wi-Fi on and give it up to a
# minute to connect.  The dashboard retries on its own if it is still not up.
lipc-set-prop com.lab126.cmd wirelessEnable 1
i=0
while [ "$(lipc-get-prop com.lab126.wifid cmState 2>/dev/null)" != "CONNECTED" ] && [ $i -lt 60 ]; do
	sleep 1
	i=$((i + 1))
done
echo "$(date) wifi: $(lipc-get-prop com.lab126.wifid cmState 2>/dev/null) after ${i}s"

# Keep the Kindle awake, hide its status bar, and freeze its window manager so
# the normal interface neither redraws over the dashboard nor reacts to input.
lipc-set-prop com.lab126.powerd preventScreenSaver 1
lipc-set-prop com.lab126.pillow disableEnablePillow disable
# Which way up the Kindle is held, for display.orientation: auto.  Asked now,
# because the window manager answers this and is about to be frozen.
ORIENT="$(lipc-get-prop com.lab126.winmgr accelerometer 2>/dev/null)"
echo "$(date) held: $ORIENT"
killall -STOP awesome

if [ "$1" = "keytest" ]; then
	"$DIR/bin/kidsdash" -config "$DIR/config.yaml" -orientation "$ORIENT" -keytest
elif [ "$1" = "nettest" ]; then
	"$DIR/bin/kidsdash" -config "$DIR/config.yaml" -orientation "$ORIENT" -nettest
else
	"$DIR/bin/kidsdash" -config "$DIR/config.yaml" -orientation "$ORIENT"
fi
echo "$(date) dashboard exited with status $?"

# Hand the Kindle back.
killall -CONT awesome
lipc-set-prop com.lab126.pillow disableEnablePillow enable
lipc-set-prop com.lab126.powerd preventScreenSaver 0
# Make the home screen repaint over the last dashboard frame.
lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.home

rm -f "$PIDFILE"
