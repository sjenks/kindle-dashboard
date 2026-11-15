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

	Calendars  []string          `yaml:"calendars"`
	EventsFile string            `yaml:"events_file"`
	DaysAhead  int               `yaml:"days_ahead"`
	Keywords   map[string]string `yaml:"keywords"`

	WeatherMinutes  int `yaml:"weather_minutes"`
	CalendarMinutes int `yaml:"calendar_minutes"`

	Lock struct {
		Enabled         bool     `yaml:"enabled"`
		UnlockSequence  []string `yaml:"unlock_sequence"`
		TimeoutSeconds  int      `yaml:"timeout_seconds"`
		GrabPowerButton bool     `yaml:"grab_power_button"`
	} `yaml:"lock"`

	Display struct {
		Width            int `yaml:"width"`
		Height           int `yaml:"height"`
		FullRefreshEvery int `yaml:"full_refresh_every"`
		// "portrait", "landscape" or "landscape-flipped"
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
	if cfg.EventsFile == "" {
		cfg.EventsFile = "events.yaml"
	}
	if cfg.DaysAhead <= 0 {
		cfg.DaysAhead = 60
	}
	if cfg.WeatherMinutes <= 0 {
		cfg.WeatherMinutes = 30
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
	case "portrait", "landscape", "landscape-flipped":
	default:
		return nil, fmt.Errorf("display.orientation %q: want portrait, landscape or landscape-flipped", cfg.Display.Orientation)
	}
	if cfg.Display.FullRefreshEvery <= 0 {
		cfg.Display.FullRefreshEvery = 30
	}
	return cfg, nil
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
