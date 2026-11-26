//go:build linux

package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errTooSoon = errors.New("next wake-up is too close to be worth sleeping")

var rtcLogged bool

// wakeAlarms lists the real-time clocks that can wake the Kindle from sleep
// (the ones marked as wake-up sources), falling back to every clock there is.
func wakeAlarms() []string {
	all, _ := filepath.Glob("/sys/class/rtc/rtc*/wakealarm")
	var wake []string
	for _, p := range all {
		w, err := os.ReadFile(filepath.Join(filepath.Dir(p), "device", "power", "wakeup"))
		if err == nil && strings.TrimSpace(string(w)) == "enabled" {
			wake = append(wake, p)
		}
	}
	if len(wake) > 0 {
		return wake
	}
	return all
}

// clearWakeAlarms cancels any alarm left set.
func clearWakeAlarms() {
	for _, p := range wakeAlarms() {
		os.WriteFile(p, []byte("0"), 0)
	}
}

// suspendUntil puts the Kindle into its deep sleep (suspend to RAM) with an
// alarm on the real-time clock to wake it at t.  The e-ink screen keeps the
// last picture without power.  It returns once the Kindle is awake again;
// early is true when something other than the alarm woke it (the power
// button).
func suspendUntil(t time.Time) (early bool, err error) {
	secs := int(math.Ceil(time.Until(t.Round(0)).Seconds())) // not a moment early
	if secs < 10 {
		return false, errTooSoon
	}

	// Without an alarm, nothing but the power button would wake the Kindle,
	// so sleep only if one was set.
	var armed []string
	for _, p := range wakeAlarms() {
		os.WriteFile(p, []byte("0"), 0) // an alarm still set refuses a new one
		if os.WriteFile(p, []byte(fmt.Sprintf("+%d", secs)), 0) == nil {
			armed = append(armed, filepath.Base(filepath.Dir(p)))
		}
	}
	if len(armed) == 0 {
		return false, errors.New("could not set a wake-up alarm on any real-time clock")
	}
	if !rtcLogged {
		log.Printf("sleep: wake-up alarm set on %s", strings.Join(armed, ", "))
		rtcLogged = true
	}
	defer clearWakeAlarms()

	f, err := os.OpenFile("/sys/power/state", os.O_WRONLY, 0)
	if err != nil {
		return false, err
	}
	// The write returns once the Kindle has woken up again.
	_, err = f.WriteString("mem")
	f.Close()
	if err != nil {
		return false, err
	}
	return time.Now().Round(0).Before(t.Round(0).Add(-3 * time.Second)), nil
}
