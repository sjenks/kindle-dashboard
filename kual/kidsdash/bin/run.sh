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

# Remember how Wi-Fi and the front light were, to put them back afterwards:
# the dashboard switches Wi-Fi on only while fetching, and the light off.
# They are kept in a file until they have been put back.  If the last run
# was cut short (plugging the Kindle into a computer stops it, and this
# script with it), the file still holds the settings from before that run;
# what the Kindle has now is only what the dashboard left behind.
RESTORE="$DIR/cache/restore.txt"
if [ -s "$RESTORE" ]; then
	read WIFI_WAS FL_WAS < "$RESTORE"
	echo "$(date) last run was cut short; keeping its saved settings"
else
	WIFI_WAS="$(lipc-get-prop com.lab126.cmd wirelessEnable 2>/dev/null)"
	FL_WAS="$(lipc-get-prop com.lab126.powerd flIntensity 2>/dev/null)"
	mkdir -p "$DIR/cache"
	echo "${WIFI_WAS:-1} ${FL_WAS:--}" > "$RESTORE"
fi
case "$WIFI_WAS" in 0|1) ;; *) WIFI_WAS=1 ;; esac
case "$FL_WAS" in ''|*[!0-9]*) FL_WAS= ;; esac
echo "$(date) battery: $(lipc-get-prop com.lab126.powerd battLevel 2>/dev/null)%, wifi was: $WIFI_WAS, light was: ${FL_WAS:-?}"

# Stop the Kindle from going to sleep on its own (the dashboard puts it to
# sleep itself, with an alarm to wake it for the next redraw), hide its
# status bar, and freeze its window manager so the normal interface neither
# redraws over the dashboard nor reacts to input.
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
lipc-set-prop -i com.lab126.cmd wirelessEnable "$WIFI_WAS"
[ -n "$FL_WAS" ] && lipc-set-prop -i com.lab126.powerd flIntensity "$FL_WAS"
rm -f "$RESTORE"
echo "$(date) battery: $(lipc-get-prop com.lab126.powerd battLevel 2>/dev/null)%"
# Make the home screen repaint over the last dashboard frame.
lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.home

rm -f "$PIDFILE"
