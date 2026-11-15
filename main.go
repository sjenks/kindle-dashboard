// kidsdash is a picture dashboard for a child, made for a jailbroken Kindle
// Oasis: day, clock, season, weather, what to wear, and what is coming up.
package main

import (
	"bufio"
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
	keytest := flag.Bool("keytest", false, "show which buttons are pressed for 30 seconds, then exit")
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
			app.fetch(time.Time{})
		}
		app.events = loadEvents(cfg, now)
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
	keys, release := startInput(cfg)
	defer release()

	if *keytest {
		runKeytest(cfg, disp, keys)
		return
	}
	(&App{cfg: cfg, disp: disp}).run(keys)
}

type App struct {
	cfg  *Config
	disp *Display

	mu      sync.Mutex
	weather *Weather
	events  []Event

	weatherTried, calendarsTried time.Time
	weatherOK, calendarsOK       bool
}

func (a *App) scene(now time.Time) Scene {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Scene{Now: now, Weather: a.weather, Events: a.events}
}

// fetch refreshes weather and calendars when they are due.  It reports
// whether anything was fetched.  A failed attempt is retried sooner.
func (a *App) fetch(now time.Time) bool {
	due := func(tried time.Time, ok bool, minutes int) bool {
		if now.IsZero() || tried.IsZero() {
			return true
		}
		if !ok {
			minutes = 3
		}
		return now.Sub(tried) >= time.Duration(minutes)*time.Minute
	}

	changed := false
	if due(a.weatherTried, a.weatherOK, a.cfg.WeatherMinutes) {
		a.weatherTried = now
		w, err := fetchWeather(a.cfg)
		a.weatherOK = err == nil
		if err != nil {
			log.Printf("weather: %v", err)
		} else {
			a.mu.Lock()
			a.weather = w
			a.mu.Unlock()
			changed = true
		}
	}
	if len(a.cfg.Calendars) > 0 && due(a.calendarsTried, a.calendarsOK, a.cfg.CalendarMinutes) {
		a.calendarsTried = now
		a.calendarsOK = fetchCalendars(a.cfg)
		changed = true
	}
	return changed
}

func (a *App) reloadEvents(now time.Time) {
	events := loadEvents(a.cfg, now)
	a.mu.Lock()
	a.events = events
	a.mu.Unlock()
}

func (a *App) run(keys <-chan uint16) {
	cfg := a.cfg
	now := func() time.Time { return time.Now().In(cfg.loc) }

	a.weather = loadCachedWeather(cfg)
	a.reloadEvents(now())

	// Background: keep weather and events current, and nudge a redraw.
	dataCh := make(chan struct{}, 1)
	go func() {
		eventsLoaded := now()
		for {
			t := now()
			fetched := a.fetch(t)
			if fetched || t.Sub(eventsLoaded) >= 10*time.Minute || !sameDay(t, eventsLoaded) {
				a.reloadEvents(t)
				eventsLoaded = t
				select {
				case dataCh <- struct{}{}:
				default:
				}
			}
			time.Sleep(time.Minute)
		}
	}()

	wakeCh := watchWake()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	matcher := newSeqMatcher(cfg.Lock.UnlockSequence, time.Duration(cfg.Lock.TimeoutSeconds)*time.Second)

	frames := 0
	draw := func(full bool) {
		if frames%cfg.Display.FullRefreshEvery == 0 {
			full = true
		}
		frames++
		if err := a.disp.Show(render(cfg, a.scene(now()), a.disp.W, a.disp.H), full); err != nil {
			log.Printf("display: %v", err)
		}
	}

	log.Printf("dashboard running; lock enabled=%v", cfg.Lock.Enabled)
	draw(true)
	for {
		// Redraw just after each minute turns over, so the clock stays right.
		t := now()
		next := t.Truncate(time.Minute).Add(time.Minute + time.Second)
		timer := time.NewTimer(next.Sub(t))

		select {
		case <-timer.C:
			draw(false)
		case <-dataCh:
			draw(false)
		case <-wakeCh:
			// The Kindle's own screensaver was just on screen; repaint over it.
			time.Sleep(1500 * time.Millisecond)
			draw(true)
		case code := <-keys:
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
