package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Timezone string `yaml:"timezone"`
	Location struct {
		Latitude  float64 `yaml:"latitude"`
		Longitude float64 `yaml:"longitude"`
	} `yaml:"location"`
	Units string `yaml:"units"` // "fahrenheit" or "celsius"
	Clock string `yaml:"clock"` // "12h" or "24h"
	// "sunday" or "monday"
	WeekStart string `yaml:"week_start"`

	Calendars []string `yaml:"calendars"`
	// A file of calendar links, one per line, kept out of git because each
	// link grants access to its calendar. Added to Calendars.
	CalendarsFile string            `yaml:"calendars_file"`
	EventsFile    string            `yaml:"events_file"`
	DaysAhead     int               `yaml:"days_ahead"`
	Keywords      map[string]string `yaml:"keywords"`

	WeatherMinutes  int `yaml:"weather_minutes"`
	CalendarMinutes int `yaml:"calendar_minutes"`

	Lock struct {
		Enabled         bool     `yaml:"enabled"`
		UnlockSequence  []string `yaml:"unlock_sequence"`
		TimeoutSeconds  int      `yaml:"timeout_seconds"`
		GrabPowerButton bool     `yaml:"grab_power_button"`
	} `yaml:"lock"`

	Power struct {
		// Deep sleep between redraws, woken by an alarm.
		Sleep bool `yaml:"sleep"`
		// Keep Wi-Fi on all the time instead of only while fetching.
		WifiAlwaysOn bool `yaml:"wifi_always_on"`
		// How long to stay awake after the power button wakes the Kindle.
		StayAwakeSeconds int `yaml:"stay_awake_seconds"`
	} `yaml:"power"`

	Display struct {
		Width  int `yaml:"width"`
		Height int `yaml:"height"`
		// Redraw every N minutes (the clock moves in steps of N).
		ClockMinutes int `yaml:"clock_minutes"`
		// Flashing clean-up refresh every N minutes.
		FullRefreshEvery int `yaml:"full_refresh_every"`
		// Front light while the dashboard shows: 0 (off) to 24, or -1 to leave it.
		Frontlight *int `yaml:"frontlight"`
		// "auto" (follow the Kindle as it is turned), "portrait", "landscape"
		// or "landscape-flipped"
		Orientation string `yaml:"orientation"`
	} `yaml:"display"`

	dir string         // directory holding the config file; other paths are relative to it
	loc *time.Location // resolved Timezone
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	cfg.Lock.Enabled = true
	cfg.Power.Sleep = true
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.dir = filepath.Dir(path)

	if cfg.Timezone == "" {
		cfg.Timezone = "UTC"
	}
	if cfg.loc, err = time.LoadLocation(cfg.Timezone); err != nil {
		return nil, fmt.Errorf("timezone %q: %w", cfg.Timezone, err)
	}
	if cfg.Units != "celsius" {
		cfg.Units = "fahrenheit"
	}
	if cfg.Clock != "24h" {
		cfg.Clock = "12h"
	}
	if cfg.CalendarsFile == "" {
		cfg.CalendarsFile = "calendars.txt"
	}
	extra, err := readCalendarsFile(cfg.path(cfg.CalendarsFile))
	if err != nil {
		return nil, err
	}
	cfg.Calendars = append(cfg.Calendars, extra...)
	if cfg.EventsFile == "" {
		cfg.EventsFile = "events.yaml"
	}
	if cfg.DaysAhead <= 0 {
		cfg.DaysAhead = 60
	}
	if cfg.WeatherMinutes <= 0 {
		cfg.WeatherMinutes = 60
	}
	if cfg.CalendarMinutes <= 0 {
		cfg.CalendarMinutes = 60
	}
	if len(cfg.Lock.UnlockSequence) == 0 {
		cfg.Lock.UnlockSequence = []string{"next", "next", "prev", "prev", "next", "prev", "next", "next"}
	}
	if cfg.Lock.TimeoutSeconds <= 0 {
		cfg.Lock.TimeoutSeconds = 10
	}
	switch cfg.Display.Orientation {
	case "":
		cfg.Display.Orientation = "portrait"
	case "auto", "portrait", "landscape", "landscape-flipped":
	default:
		return nil, fmt.Errorf("display.orientation %q: want auto, portrait, landscape or landscape-flipped", cfg.Display.Orientation)
	}
	if cfg.Display.FullRefreshEvery <= 0 {
		cfg.Display.FullRefreshEvery = 30
	}
	if cfg.Display.ClockMinutes <= 0 {
		cfg.Display.ClockMinutes = 5
	}
	if cfg.Display.ClockMinutes > 60 {
		cfg.Display.ClockMinutes = 60
	}
	if cfg.Display.Frontlight == nil {
		off := 0
		cfg.Display.Frontlight = &off
	}
	if *cfg.Display.Frontlight > 24 {
		*cfg.Display.Frontlight = 24
	}
	if cfg.Power.StayAwakeSeconds <= 0 {
		cfg.Power.StayAwakeSeconds = 60
	}
	return cfg, nil
}

// autoRotate reports whether the dashboard follows the Kindle as it is turned.
func (c *Config) autoRotate() bool {
	return c.Display.Orientation == "auto"
}

// landscape reports whether the dashboard should be drawn sideways.
func (c *Config) landscape() bool {
	return strings.HasPrefix(c.Display.Orientation, "landscape")
}

// path resolves a file name relative to the config directory.
func (c *Config) path(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(c.dir, name)
}

func (c *Config) cachePath(name string) string {
	dir := c.path("cache")
	os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, name)
}

// readCalendarsFile reads calendar links, one per line; blank lines and lines
// starting with # are skipped. A missing file means no extra calendars.
func readCalendarsFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, nil
}
