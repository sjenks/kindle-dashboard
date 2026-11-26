package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testICS = `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:one
DTSTART;VALUE=DATE:20261120
DTEND;VALUE=DATE:20261121
SUMMARY:Zoo trip\, with friends
END:VEVENT
BEGIN:VEVENT
UID:utc
DTSTART:20261116T200000Z
SUMMARY:Dentist
BEGIN:VALARM
TRIGGER:-PT30M
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:weekly
DTSTART;TZID=America/New_York:20260105T160000
RRULE:FREQ=WEEKLY;BYDAY=MO,WE
EXDATE;TZID=America/New_York:20261118T160000
SUMMARY:Swim
END:VEVENT
BEGIN:VEVENT
UID:weekly
RECURRENCE-ID;TZID=America/New_York:20261116T160000
DTSTART;TZID=America/New_York:20261117T170000
SUMMARY:Swim (moved)
END:VEVENT
BEGIN:VEVENT
UID:bday
DTSTART;VALUE=DATE:20191122
RRULE:FREQ=YEARLY
SUMMARY:Mia's birthday
END:VEVENT
BEGIN:VEVENT
UID:monthly
DTSTART;TZID=America/New_York:20260113T100000
RRULE:FREQ=MONTHLY;BYDAY=3TH
SUMMARY:Library
 story time
END:VEVENT
BEGIN:VEVENT
UID:ended
DTSTART;VALUE=DATE:20260101
RRULE:FREQ=DAILY;COUNT=5
SUMMARY:Finished long ago
END:VEVENT
BEGIN:VEVENT
UID:trip
DTSTART;VALUE=DATE:20261110
DTEND;VALUE=DATE:20261117
SUMMARY:Vacation
END:VEVENT
END:VCALENDAR
`

func TestParseICS(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	from := time.Date(2026, 11, 14, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 10)

	var got []string
	for _, e := range parseICS([]byte(strings.ReplaceAll(testICS, "\n", "\r\n")), loc, from, to) {
		got = append(got, e.When.Format("01-02 15:04 ")+e.Title)
	}
	want := []string{
		"11-20 00:00 Zoo trip, with friends",
		"11-16 15:00 Dentist",
		"11-23 16:00 Swim", // 16th moved, 18th excluded
		"11-17 17:00 Swim (moved)",
		"11-22 00:00 Mia's birthday",
		"11-19 10:00 Librarystory time",
		"11-14 00:00 Vacation", // under way: shown as today
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSeqMatcher(t *testing.T) {
	seq := []string{"next", "next", "back"}
	t0 := time.Unix(1000, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

	m := newSeqMatcher(seq, 5*time.Second)
	if m.press(109, at(0)) || m.press(109, at(300)) || !m.press(104, at(600)) {
		t.Error("correct sequence should unlock on the last press")
	}

	// A wrong press spoils the attempt, even if the right keys follow at once.
	m = newSeqMatcher(seq, 5*time.Second)
	presses := []uint16{104, 109, 109, 104}
	for i, k := range presses {
		if m.press(k, at(i*300)) {
			t.Error("mashing should not unlock without a pause")
		}
	}
	// ...but after a quiet pause a fresh attempt works.
	if m.press(109, at(5000)) || m.press(109, at(5300)) || !m.press(104, at(5600)) {
		t.Error("sequence after a pause should unlock")
	}

	// Too slow overall.
	m = newSeqMatcher(seq, 2*time.Second)
	if m.press(109, at(0)) || m.press(109, at(1500)) || m.press(104, at(3000)) {
		t.Error("slow sequence should not unlock")
	}
}

func TestRotate90(t *testing.T) {
	// A 3x2 page:  1 2 3
	//              4 5 6
	src := image.NewGray(image.Rect(0, 0, 3, 2))
	copy(src.Pix, []uint8{1, 2, 3, 4, 5, 6})

	cw := rotate90(src, true)
	if cw.Bounds().Dx() != 2 || cw.Bounds().Dy() != 3 || string(cw.Pix) != string([]uint8{4, 1, 5, 2, 6, 3}) {
		t.Errorf("clockwise: got %v", cw.Pix)
	}
	ccw := rotate90(src, false)
	if string(ccw.Pix) != string([]uint8{3, 6, 2, 5, 1, 4}) {
		t.Errorf("counter-clockwise: got %v", ccw.Pix)
	}
}

func TestPictureFor(t *testing.T) {
	cfg := &Config{Keywords: map[string]string{"Nana": "grandma"}}
	for title, want := range map[string]string{
		"Mia's Birthday party":  "cake",
		"Art class":             "art",
		"Start of term":         "calendar",
		"Lunch with nana":       "grandma",
		"Dentist appointment":   "dentist",
		"Something unknowable?": "calendar",
	} {
		if got := pictureFor(Event{Title: title}, cfg); got != want {
			t.Errorf("pictureFor(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestCalendarsFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("calendars: [https://a.example/a.ics]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "calendars.txt"), []byte("# family\nwebcal://b.example/b.ics\n\n  https://c.example/c.ics  \n"), 0o644)
	cfg, err := loadConfig(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://a.example/a.ics", "webcal://b.example/b.ics", "https://c.example/c.ics"}
	if strings.Join(cfg.Calendars, " ") != strings.Join(want, " ") {
		t.Errorf("Calendars = %v, want %v", cfg.Calendars, want)
	}

	os.Remove(filepath.Join(dir, "calendars.txt"))
	if cfg, err = loadConfig(filepath.Join(dir, "config.yaml")); err != nil || len(cfg.Calendars) != 1 {
		t.Errorf("without calendars.txt: %v, %v", cfg.Calendars, err)
	}
}

func TestRotate180(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 3, 2))
	copy(src.Pix, []byte{1, 2, 3, 4, 5, 6})
	got := rotate180(src)
	if want := []byte{6, 5, 4, 3, 2, 1}; string(got.Pix) != string(want) {
		t.Errorf("rotate180 = %v, want %v", got.Pix, want)
	}
}

func TestAutoRotation(t *testing.T) {
	// Each frame must be turned against the way the Kindle is turned, counted
	// from the way its screen faced at start.
	cases := []struct {
		start string
		now   int // quarter turns clockwise from upright, from the motion sensor
		want  int // clockwise quarter turns applied to the frame
	}{
		{"U", 0, 0},
		{"U", 1, 3},
		{"U", 2, 2},
		{"U", 3, 1},
		{"D", 2, 0}, // started upside down: the home screen was upside down too
		{"D", 0, 2},
		{"R", 1, 3}, // started sideways: the home screen was still upright
		{"", 0, 0},  // unknown: assume upright
	}
	for _, c := range cases {
		base, _ := rotationBase(c.start)
		if got := frameTurns(c.now, base); got != c.want {
			t.Errorf("start %q, held %d: frameTurns = %d, want %d", c.start, c.now, got, c.want)
		}
	}
	if _, held := rotationBase("R"); held != 1 {
		t.Errorf("rotationBase(R) held = %d, want 1", held)
	}

	d := &Display{nativeW: 1264, nativeH: 1680}
	for turns, landscape := range []bool{false, true, false, true} {
		d.setTurns(turns)
		if (d.W > d.H) != landscape {
			t.Errorf("setTurns(%d): page %dx%d", turns, d.W, d.H)
		}
	}
	if d.setTurns(-1); d.turns != 3 {
		t.Errorf("setTurns(-1) = %d, want 3", d.turns)
	}
}

func TestNextTick(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	at := func(s string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	cases := []struct {
		now   string
		every int
		want  string
	}{
		{"2026-11-25 10:03:20", 5, "2026-11-25 10:05:01"},
		{"2026-11-25 10:05:00", 5, "2026-11-25 10:10:01"},
		{"2026-11-25 10:05:01", 5, "2026-11-25 10:10:01"},
		{"2026-11-25 23:58:00", 5, "2026-11-26 00:00:01"},
		{"2026-11-25 10:03:20", 1, "2026-11-25 10:04:01"},
		{"2026-11-01 01:58:00", 5, "2026-11-01 01:00:01"}, // clocks go back at 2:00
		{"2026-03-08 03:01:00", 5, "2026-03-08 03:05:01"}, // the day clocks go forward
	}
	for _, c := range cases {
		got := nextTick(at(c.now), c.every)
		if c.now == "2026-11-01 01:58:00" {
			// 01:58 PDT is followed by 01:00:01 PST, an hour of wall time later.
			if d := got.Sub(at(c.now)); d != 2*time.Minute+time.Second {
				t.Errorf("nextTick(%s) is %v later, want 2m1s", c.now, d)
			}
			continue
		}
		if !got.Equal(at(c.want)) {
			t.Errorf("nextTick(%s, %d) = %s, want %s", c.now, c.every, got.Format("2006-01-02 15:04:05"), c.want)
		}
	}
}

func TestSourceDue(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 10, 0, 1, 0, time.UTC)
	min := func(m float64) time.Time { return t0.Add(time.Duration(m * float64(time.Minute))) }

	var s source
	if !s.due(t0, 60) {
		t.Error("never fetched: should be due")
	}
	s.done(t0, true)
	if s.due(min(55), 60) || !s.due(min(59.99), 60) {
		t.Error("after a success: due again an hour later (give or take the redraw)")
	}

	// Failures back off: 5, 10, 20, 40, then the normal hour.
	s = source{}
	tried := t0
	for i, wait := range []float64{5, 10, 20, 40, 60, 60} {
		s.done(tried, false)
		if s.due(tried.Add(time.Duration(wait-1.5)*time.Minute), 60) {
			t.Errorf("failure %d: due before %v minutes", i+1, wait)
		}
		next := tried.Add(time.Duration(wait) * time.Minute)
		if !s.due(next, 60) {
			t.Errorf("failure %d: not due after %v minutes", i+1, wait)
		}
		tried = next
	}
	s.done(tried, true)
	if s.fails != 0 {
		t.Error("a success should reset the backoff")
	}
}

func TestPowerDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("timezone: UTC\n"), 0o644)
	cfg, err := loadConfig(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Power.Sleep || cfg.Power.WifiAlwaysOn || cfg.Power.StayAwakeSeconds != 60 ||
		cfg.Display.ClockMinutes != 5 || *cfg.Display.Frontlight != 0 || cfg.WeatherMinutes != 60 {
		t.Errorf("defaults: power %+v, clock %d, light %d, weather %d",
			cfg.Power, cfg.Display.ClockMinutes, *cfg.Display.Frontlight, cfg.WeatherMinutes)
	}

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("power: {sleep: false}\ndisplay: {frontlight: -1}\n"), 0o644)
	if cfg, err = loadConfig(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if cfg.Power.Sleep || *cfg.Display.Frontlight != -1 {
		t.Errorf("overrides not kept: sleep %v, light %d", cfg.Power.Sleep, *cfg.Display.Frontlight)
	}
}

func TestBatteryTracker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "battery.json")
	t0 := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	hours := func(h float64) time.Time { return t0.Add(time.Duration(h * float64(time.Hour))) }

	b := newBatteryTracker(path)
	if got := b.update(t0, 100, true); !got.Charging || got.Left != 0 {
		t.Errorf("charging: %+v", got)
	}
	// Unplugged: no estimate until it has dropped a little over a few hours.
	for h := 0.25; h < 3; h += 0.25 {
		if got := b.update(hours(h), 100-int(h), false); got.Left != 0 {
			t.Errorf("at %vh: estimate too early: %+v", h, got)
		}
	}
	// 1% an hour, from 100% at 8:00: at 4h and 96%, 96 hours left.
	if got := b.update(hours(4), 96, false); got.Left != 96*time.Hour {
		t.Errorf("at 4h: Left = %v, want 96h", got.Left)
	}

	// Starting again keeps the baseline...
	b = newBatteryTracker(path)
	b.update(hours(4.5), 96, false)
	if got := b.update(hours(5), 95, false); got.Left != 95*time.Hour {
		t.Errorf("after restart: Left = %v, want 95h", got.Left)
	}
	// ...unless the dashboard was not running for a while.
	b = newBatteryTracker(path)
	if got := b.update(hours(9), 80, false); got.Left != 0 {
		t.Errorf("after a gap: %+v, want no estimate", got)
	}
}

func TestBatteryLabel(t *testing.T) {
	cases := []struct {
		b    Battery
		want string
	}{
		{Battery{Level: 82}, "82%"},
		{Battery{Level: 82, Charging: true}, "82%  charging"},
		{Battery{Level: 82, Left: 9*24*time.Hour + 5*time.Hour}, "82%  about 9 days left"},
		{Battery{Level: 10, Left: 20 * time.Hour}, "10%  about 20 hours left"},
		{Battery{Level: 3, Left: 70 * time.Minute}, "3%  about 1 hour left"},
		{Battery{Level: 1, Left: 30 * time.Minute}, "1%  charge now"},
	}
	for _, c := range cases {
		if got := batteryLabel(c.b); got != c.want {
			t.Errorf("batteryLabel(%+v) = %q, want %q", c.b, got, c.want)
		}
	}
}
