# Kids Dashboard for Kindle Oasis

A picture dashboard for a child, running on a jailbroken Kindle Oasis: the day
of the week, an analog clock, the season, the weather with what to wear, the
next few days, and what is coming up ("3 sleeps").

Everything is drawn on the Kindle by one self-contained program. While it is
showing, the touchscreen is switched off; a secret sequence on the page-turn
buttons returns to the normal Kindle.

## Set it up

1. Edit `config.yaml`: timezone, latitude/longitude, units and the unlock
   sequence. Private calendar (ICS) links go in `calendars.txt`, one per line,
   not in `config.yaml`: each link is a password to its calendar, and
   `calendars.txt` is git-ignored so it never reaches GitHub.
2. Edit `events.yaml` for birthdays and other events. Photos of your own
   (`grandma.jpg`) go in a `pictures` folder next to it.
3. See how it looks on this PC:

   ```powershell
   .\scripts\preview.ps1                       # writes preview.png
   .\scripts\preview.ps1 -now 2026-12-24T08:30 # pretend it is another time
   ```

4. Build and install:

   ```powershell
   .\scripts\build.ps1
   ```

   Plug the Kindle in over USB and copy `dist\extensions\kidsdash` into the
   `extensions` folder on the Kindle. This needs KUAL (the Kindle Unified
   Application Launcher) installed.

5. On the Kindle: KUAL > Kids Dashboard > **Button test** shows what each
   page-turn button is called (`prev` / `next`), so you can check your unlock
   sequence. Then **Start**.

## The child lock

- The touchscreen is grabbed exclusively: taps reach nothing.
- To unlock: stop pressing for two seconds, then enter `unlock_sequence` on
  the page-turn buttons within `timeout_seconds`. One wrong press spoils the
  attempt until the next two-second pause, so button mashing does not get
  through.
- The power button still puts the Kindle to sleep; the dashboard redraws on
  wake. `grab_power_button: true` switches it off too (experimental).
- Escape hatch: holding the power button for about 40 seconds restarts the
  Kindle, which comes back as a normal Kindle. The lock cannot outlive the
  dashboard process.

## Portrait or landscape

`display.orientation` in `config.yaml` is `auto`, `portrait`, `landscape` or
`landscape-flipped`.

- `auto` follows the Kindle as it is turned, using the Oasis motion sensor:
  portrait either way up, and landscape with its own layout. It redraws once
  the Kindle has been still for a second.
- The others fix the orientation. If landscape comes out upside down for the
  way the Kindle is propped up, switch to the other landscape setting.

The page-turn buttons keep their names whichever way the Kindle is turned, so
the unlock sequence is always pressed on the same physical buttons.

## Good to know

- The dashboard keeps the Kindle awake with Wi-Fi on, so leave it on a charger.
- Weather comes from [Open-Meteo](https://open-meteo.com) (no account needed).
  Calendars are read from private ICS links; repeating events are supported
  for the common cases (daily, weekly, monthly, yearly).
- If [FBInk](https://github.com/NiLuJe/FBInk) is present (it ships with
  KOReader, or drop the binary in `extensions/kidsdash/bin/`), it is used for
  drawing; otherwise the Kindle's built-in `eips` is.
- Troubleshooting: `extensions/kidsdash/kidsdash.log` on the Kindle.
- Add pictures by adding a line to `assets\manifest.txt` and rebuilding.

## Development

Go lives in `.tools\` (nothing is installed system-wide). `. .\scripts\env.ps1`
puts it on the path for the current shell; then `go test ./...`.

Pictures are from [OpenMoji](https://openmoji.org) (CC BY-SA 4.0); the font is
Lilita One (OFL).
