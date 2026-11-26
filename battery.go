package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
)

// Battery is what the dashboard shows about the battery.
type Battery struct {
	Level    int           // percent
	Charging bool          //
	Left     time.Duration // rough time until empty; 0 while there is no estimate yet
}

// batteryTracker works out how fast the battery is running down, from the
// level now and the level when it was last unplugged (or when the dashboard
// started, if the Kindle was in use in between).  It is kept in the cache
// folder, so unlocking and starting again does not lose it.
type batteryTracker struct {
	path string

	SinceAt    time.Time `json:"since_at"`
	SinceLevel int       `json:"since_level"`
	LastAt     time.Time `json:"last_at"`
	LastLevel  int       `json:"last_level"`

	savedAt time.Time
}

func newBatteryTracker(path string) *batteryTracker {
	b := &batteryTracker{path: path}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, b)
	}
	return b
}

// An estimate needs the level to have dropped this much over at least this
// long; before that, it would mostly be noise.
const (
	batteryMinDrop = 2
	batteryMinTime = 3 * time.Hour
	// Longer than this between readings, the dashboard was not running and
	// the Kindle may have been read, charged or left off: start afresh.
	batteryGap = 2 * time.Hour
)

// update takes a new reading and returns what to show.
func (b *batteryTracker) update(now time.Time, level int, charging bool) Battery {
	fresh := b.SinceAt.IsZero() || charging || level > b.LastLevel || now.Sub(b.LastAt) > batteryGap || now.Before(b.LastAt)
	if fresh {
		b.SinceAt, b.SinceLevel = now, level
	}
	b.LastAt, b.LastLevel = now, level
	if fresh || now.Sub(b.savedAt) >= 30*time.Minute {
		b.save(now)
	}

	bat := Battery{Level: level, Charging: charging}
	drop := b.SinceLevel - level
	elapsed := now.Sub(b.SinceAt)
	if !charging && drop >= batteryMinDrop && elapsed >= batteryMinTime {
		perHour := float64(drop) / elapsed.Hours()
		bat.Left = time.Duration(float64(level) / perHour * float64(time.Hour))
	}
	return bat
}

func (b *batteryTracker) save(now time.Time) {
	if data, err := json.Marshal(b); err == nil && os.WriteFile(b.path, data, 0o644) == nil {
		b.savedAt = now
	}
}

// readBattery asks powerd for the battery level; ok is false off the Kindle.
func readBattery() (level int, charging, ok bool) {
	s, err := lipcGet("com.lab126.powerd", "battLevel")
	if err != nil {
		return 0, false, false
	}
	level, err = strconv.Atoi(s)
	if err != nil {
		return 0, false, false
	}
	c, _ := lipcGet("com.lab126.powerd", "isCharging")
	return level, c == "1", true
}

// batteryLabel is the text next to the battery: "82%", "82% charging",
// or "82%  about 9 days left".
func batteryLabel(b Battery) string {
	s := fmt.Sprintf("%d%%", b.Level)
	switch {
	case b.Charging:
		return s + "  charging"
	case b.Left <= 0:
		return s
	case b.Left < time.Hour:
		return s + "  charge now"
	case b.Left < 36*time.Hour:
		h := int(math.Round(b.Left.Hours()))
		return fmt.Sprintf("%s  about %d hour%s left", s, h, plural(h))
	default:
		d := int(math.Round(b.Left.Hours() / 24))
		return fmt.Sprintf("%s  about %d day%s left", s, d, plural(d))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
