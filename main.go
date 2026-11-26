// kidsdash is a picture dashboard for a child, made for a jailbroken Kindle
// Oasis: day, clock, season, weather, what to wear, and what is coming up.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "time/tzdata" // the Kindle has no usable timezone database

	_ "golang.org/x/crypto/x509roots/fallback" // nor a dependable CA bundle
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	preview := flag.String("preview", "", "render one frame to this PNG file and exit (for trying layouts on a PC)")
	nowFlag := flag.String("now", "", "pretend it is this time, e.g. 2026-12-24T08:30 (with -preview)")
	offline := flag.Bool("offline", false, "with -preview: use cached weather and calendars only")
	batteryFlag := flag.Int("battery", -1, "with -preview: show the battery at this percentage")
	batteryDays := flag.Float64("battery-days", 0, "with -preview and -battery: and this many days left")
	keytest := flag.Bool("keytest", false, "show which buttons are pressed for 30 seconds, then exit")
	nettest := flag.Bool("nettest", false, "try reaching the weather service a few ways, show the results, then exit")
	startOrient := flag.String("orientation", "", "which way up the Kindle is held at start (U, R, D or L, from lipc com.lab126.winmgr accelerometer); used with display.orientation: auto")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if *preview != "" {
		now := time.Now().In(cfg.loc)
		if *nowFlag != "" {
			if now, err = time.ParseInLocation("2006-01-02T15:04", *nowFlag, cfg.loc); err != nil {
				log.Fatalf("-now: %v", err)
			}
		}
		app := &App{cfg: cfg}
		app.weather = loadCachedWeather(cfg)
		if !*offline {
			app.fetch(time.Time{}, true)
		}
		app.events = loadEvents(cfg, now)
		if *batteryFlag >= 0 {
			app.battery = &Battery{Level: *batteryFlag, Left: time.Duration(*batteryDays * 24 * float64(time.Hour))}
		}
		w, h := cfg.Display.Width, cfg.Display.Height
		if w == 0 || h == 0 {
			w, h = 1264, 1680
		}
		if (w > h) != cfg.landscape() {
			w, h = h, w
		}
		if err := writePNG(*preview, render(cfg, app.scene(now), w, h)); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", *preview)
		return
	}

	disp := openDisplay(cfg)
	keys, orient, release := startInput(cfg)
	defer release()

	if *keytest {
		runKeytest(cfg, disp, keys)
		return
	}
	if *nettest {
		runNettest(cfg, disp)
		return
	}
	(&App{cfg: cfg, disp: disp}).run(keys, orient, *startOrient)
}

type App struct {
	cfg  *Config
	disp *Display

	mu      sync.Mutex
	weather *Weather
	events  []Event
	battery *Battery

	weatherSrc, calendarSrc source
}

// source tracks when one kind of data (weather, calendars) was last fetched.
type source struct {
	tried time.Time
	fails int // failed attempts in a row
}

// due reports whether it is time to fetch again: every minutes after a
// success, and sooner after a failure (5, 10, 20, 40 minutes...), so a
// network that is down does not keep the Wi-Fi busy all day.
func (s *source) due(now time.Time, minutes int) bool {
	if now.IsZero() || s.tried.IsZero() {
		return true
	}
	wait := minutes
	if s.fails > 0 && s.fails <= 5 {
		wait = min(5<<(s.fails-1), minutes)
	}
	// A minute's slack: redraws, and with them fetches, happen a second
	// or so after the clock ticks over, give or take.
	return now.Sub(s.tried) >= time.Duration(wait)*time.Minute-time.Minute
}

func (s *source) done(now time.Time, ok bool) {
	s.tried = now
	if ok {
		s.fails = 0
	} else {
		s.fails++
	}
}

func (a *App) scene(now time.Time) Scene {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Scene{Now: now, Weather: a.weather, Events: a.events, Battery: a.battery}
}

// fetchDue reports whether weather or calendars are due to be fetched.
func (a *App) fetchDue(now time.Time) bool {
	return a.weatherSrc.due(now, a.cfg.WeatherMinutes) ||
		(len(a.cfg.Calendars) > 0 && a.calendarSrc.due(now, a.cfg.CalendarMinutes))
}

// fetch refreshes weather and calendars when they are due.  It reports
// whether anything was fetched.  With online false (Wi-Fi did not come up)
// the due ones are just counted as failed.
func (a *App) fetch(now time.Time, online bool) bool {
	changed := false
	if a.weatherSrc.due(now, a.cfg.WeatherMinutes) {
		var w *Weather
		err := errors.New("no Wi-Fi")
		if online {
			w, err = fetchWeather(a.cfg)
		}
		a.weatherSrc.done(now, err == nil)
		if err != nil {
			log.Printf("weather: %v", err)
		} else {
			log.Printf("weather: ok, %.0f° code %d", w.Temp, w.Code)
			a.mu.Lock()
			a.weather = w
			a.mu.Unlock()
			changed = true
		}
	}
	if len(a.cfg.Calendars) > 0 && a.calendarSrc.due(now, a.cfg.CalendarMinutes) {
		ok := online && fetchCalendars(a.cfg)
		a.calendarSrc.done(now, ok)
		changed = changed || online
	}
	return changed
}

func (a *App) reloadEvents(now time.Time) {
	events := loadEvents(a.cfg, now)
	a.mu.Lock()
	a.events = events
	a.mu.Unlock()
}

// nextTick is when the clock is next redrawn: a second after the next
// multiple of every minutes, so 10:05:01, 10:10:01...
func nextTick(t time.Time, every int) time.Time {
	step := time.Duration(every) * time.Minute
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	n := t.Sub(midnight)/step + 1
	return midnight.Add(n*step + time.Second)
}

func (a *App) run(keys <-chan uint16, orient <-chan int, startOrient string) {
	cfg := a.cfg
	// Wall-clock time only: Go's monotonic clock stands still while the
	// Kindle sleeps, which would throw every comparison off.
	now := func() time.Time { return time.Now().Round(0).In(cfg.loc) }
	stayAwake := time.Duration(cfg.Power.StayAwakeSeconds) * time.Second
	sleepOK := cfg.Power.Sleep && onKindle()
	defer clearWakeAlarms()

	// Following the Kindle as it turns.  fbink draws the way the Kindle's own
	// screen was facing when we started, so turns are counted from there.
	base, held := 0, 0
	if cfg.autoRotate() {
		base, held = rotationBase(startOrient)
		a.disp.setTurns(frameTurns(held, base))
		log.Printf("orientation: auto, started held %q", startOrient)
	}
	var settle <-chan time.Time // fires once the Kindle has stopped moving

	setFrontlight(*cfg.Display.Frontlight)

	a.weather = loadCachedWeather(cfg)
	a.reloadEvents(now())
	eventsLoaded := now()

	// Fetching runs in the background, so the buttons keep working while
	// Wi-Fi connects; the Kindle does not sleep until it is done.
	fetching := false
	fetchDone := make(chan bool, 1)
	startFetch := func(t time.Time) {
		fetching = true
		go func() {
			online := wifiUp(time.Minute)
			changed := a.fetch(t, online)
			if !cfg.Power.WifiAlwaysOn {
				wifiDown()
			}
			fetchDone <- changed
		}()
	}

	wakeCh := watchWake()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	matcher := newSeqMatcher(cfg.Lock.UnlockSequence, time.Duration(cfg.Lock.TimeoutSeconds)*time.Second)

	battery := newBatteryTracker(cfg.cachePath("battery.json"))
	var lastFull time.Time
	draw := func(full bool) {
		t := now()
		if level, charging, ok := readBattery(); ok {
			b := battery.update(t, level, charging)
			a.mu.Lock()
			a.battery = &b
			a.mu.Unlock()
		}
		if t.Sub(lastFull) >= time.Duration(cfg.Display.FullRefreshEvery)*time.Minute-time.Minute {
			full = true
		}
		if full {
			lastFull = t
		}
		if err := a.disp.Show(render(cfg, a.scene(t), a.disp.W, a.disp.H), full); err != nil {
			log.Printf("display: %v", err)
		}
	}

	// Counted for the hourly battery line in the log.
	var sleeps, earlyWakes int
	lastReport := now()
	report := func(t time.Time) {
		level := batteryLevel()
		if a.battery != nil {
			level = batteryLabel(*a.battery)
		}
		log.Printf("battery: %s; slept %d times, woken early %d times", level, sleeps, earlyWakes)
		sleeps, earlyWakes, lastReport = 0, 0, t
	}
	report(lastReport)

	var awakeUntil time.Time   // stay awake (for the unlock sequence) until then
	var noSleepUntil time.Time // after a failed attempt, do not retry before then
	var alarmWake time.Time    // when our own alarm last woke the Kindle
	sleepFails := 0

	log.Printf("dashboard running; lock enabled=%v, sleep=%v, redraw every %d min",
		cfg.Lock.Enabled, sleepOK, cfg.Display.ClockMinutes)
	startFetch(now())
	draw(true)
	tick := nextTick(now(), cfg.Display.ClockMinutes)
	for {
		t := now()
		if !t.Before(tick) {
			tick = nextTick(t, cfg.Display.ClockMinutes)
			if !fetching && a.fetchDue(t) {
				startFetch(t)
			}
			if t.Sub(eventsLoaded) >= 30*time.Minute || !sameDay(t, eventsLoaded) {
				a.reloadEvents(t)
				eventsLoaded = t
			}
			if t.Sub(lastReport) >= time.Hour-time.Minute {
				report(t)
			}
			draw(false)
		}

		if sleepOK && !fetching && settle == nil && !t.Before(awakeUntil) && !t.Before(noSleepUntil) {
			if leaveScreenSaver() {
				// Its wake-up event repaints the dashboard; sleep after that.
				noSleepUntil = now().Add(3 * time.Second)
				continue
			}
			if a.disp.fbink == "" {
				time.Sleep(time.Second) // eips cannot wait for the screen to finish
			}
			early, err := suspendUntil(tick)
			switch {
			case err == errTooSoon:
				noSleepUntil = tick
			case err != nil:
				noSleepUntil = tick
				if sleepFails++; sleepFails <= 5 {
					log.Printf("sleep: %v", err)
				}
			case early:
				// The power button (or the Kindle being turned) woke it:
				// stay awake for a while so the unlock sequence can be
				// entered, and make sure the dashboard is what shows.
				sleeps++
				earlyWakes++
				log.Printf("sleep: woken early")
				awakeUntil = now().Add(stayAwake)
				time.Sleep(time.Second)
				if !leaveScreenSaver() {
					draw(false)
				}
			default:
				sleeps++
				alarmWake = now()
			}
			continue
		}

		// Awake: wait for the next redraw, or for the end of the awake spell.
		wake := tick
		if awakeUntil.After(t) && awakeUntil.Before(wake) {
			wake = awakeUntil
		}
		if noSleepUntil.After(t) && noSleepUntil.Before(wake) {
			wake = noSleepUntil
		}
		timer := time.NewTimer(wake.Sub(t))

		select {
		case <-timer.C:
			// Loop round: redraw if the tick is due, then perhaps sleep.
		case changed := <-fetchDone:
			fetching = false
			if changed {
				a.reloadEvents(now())
				eventsLoaded = now()
				draw(false)
			}
		case <-wakeCh:
			if now().Sub(alarmWake) < 10*time.Second {
				break // powerd noticing our own alarm wake-up: nothing to do
			}
			// The Kindle's own screensaver was just on screen; repaint over it.
			awakeUntil = now().Add(stayAwake)
			time.Sleep(1500 * time.Millisecond)
			draw(true)
		case o := <-orient:
			held = o
			settle = time.After(time.Second)
		case <-settle:
			settle = nil
			if ft := frameTurns(held, base); ft != a.disp.turns {
				log.Printf("orientation: turned %d quarter(s) clockwise from upright", held)
				a.disp.setTurns(ft)
				draw(true)
			}
		case code := <-keys:
			awakeUntil = now().Add(stayAwake)
			if matcher.press(code, time.Now()) {
				log.Printf("unlock sequence entered; exiting")
				a.disp.Show(renderMessage(cfg, a.disp.W, a.disp.H, "unlocked", "Unlocked", "Going back to the Kindle..."), true)
				time.Sleep(1500 * time.Millisecond)
				return
			}
		case sig := <-sigCh:
			log.Printf("received %v; exiting", sig)
			return
		}
		timer.Stop()
	}
}

// watchWake reports when the Kindle comes back from its screensaver or from
// sleep (someone pressed the power button), using the stock lipc-wait-event.
func watchWake() <-chan struct{} {
	ch := make(chan struct{}, 1)
	cmd := exec.Command("lipc-wait-event", "-m", "com.lab126.powerd", "outOfScreenSaver,wakeupFromSuspend,resuming")
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return ch // not on a Kindle: never fires
	}
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			log.Printf("power event: %s", sc.Text())
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		cmd.Wait()
	}()
	return ch
}

// runKeytest helps a parent find out what their Kindle's buttons are called,
// so they can write an unlock_sequence that works.
func runKeytest(cfg *Config, disp *Display, keys <-chan uint16) {
	lines := []string{"Button test", "Press the page buttons.", "This screen closes after 30 seconds."}
	show := func(full bool) {
		if err := disp.Show(renderMessage(cfg, disp.W, disp.H, "", lines...), full); err != nil {
			log.Printf("display: %v", err)
		}
	}
	show(true)
	deadline := time.After(30 * time.Second)
	for {
		select {
		case code := <-keys:
			log.Printf("keytest: code %d = %s", code, keyName(code))
			lines = append(lines, fmt.Sprintf("%s   (code %d)", keyName(code), code))
			if len(lines) > 11 {
				lines = append(lines[:3], lines[len(lines)-8:]...)
			}
			show(false)
		case <-deadline:
			return
		}
	}
}

// orientationTurns maps lipc's accelerometer letters to clockwise quarter turns.
var orientationTurns = map[string]int{"U": 0, "R": 1, "D": 2, "L": 3}

// rotationBase works out, from how the Kindle was held at start, which way
// up its screen was facing (base) and how it is held now (held), both as
// clockwise quarter turns from upright.  The Kindle's home screen and KUAL
// only ever face upright or upside down, never sideways.
func rotationBase(start string) (base, held int) {
	held = orientationTurns[start]
	if held == 2 {
		base = 2
	}
	return base, held
}

// frameTurns is how far to turn each frame so it reads upright for someone
// holding the Kindle turned held quarters clockwise, on a screen facing base.
func frameTurns(held, base int) int {
	return ((base-held)%4 + 4) % 4
}
