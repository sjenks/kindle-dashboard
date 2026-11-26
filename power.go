package main

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Talking to the Kindle's own services (powerd for power and the front
// light, wifid and cmd for Wi-Fi) through the stock lipc tools.  Off the
// Kindle the tools are missing and every call quietly fails.

func lipcGet(service, prop string) (string, error) {
	out, err := exec.Command("lipc-get-prop", service, prop).Output()
	return strings.TrimSpace(string(out)), err
}

func lipcSetInt(service, prop string, v int) error {
	out, err := exec.Command("lipc-set-prop", "-i", service, prop, strconv.Itoa(v)).CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)) + " " + err.Error())
	}
	return nil
}

// onKindle reports whether the lipc tools are here at all.
func onKindle() bool {
	_, err := exec.LookPath("lipc-get-prop")
	return err == nil
}

// wifiUp switches Wi-Fi on and waits up to timeout for it to connect.
func wifiUp(timeout time.Duration) bool {
	if !onKindle() {
		return true // a PC: assume it is online
	}
	if err := lipcSetInt("com.lab126.cmd", "wirelessEnable", 1); err != nil {
		log.Printf("wifi: switching on: %v", err)
	}
	start := time.Now()
	for {
		if s, _ := lipcGet("com.lab126.wifid", "cmState"); s == "CONNECTED" {
			return true
		}
		if time.Since(start) >= timeout {
			s, _ := lipcGet("com.lab126.wifid", "cmState")
			log.Printf("wifi: not connected after %v (%s)", timeout, s)
			return false
		}
		time.Sleep(time.Second)
	}
}

// wifiDown switches Wi-Fi off.
func wifiDown() {
	// Connections kept open for reuse would be dead the next time Wi-Fi
	// comes up, and could hang the next fetch: drop them now.
	httpClient.CloseIdleConnections()
	if !onKindle() {
		return
	}
	if err := lipcSetInt("com.lab126.cmd", "wirelessEnable", 0); err != nil {
		log.Printf("wifi: switching off: %v", err)
	}
}

// batteryLevel is the charge left in percent, or "" if it cannot be read.
func batteryLevel() string {
	level, charging, ok := readBattery()
	switch {
	case !ok:
		return ""
	case charging:
		return fmt.Sprintf("%d%% (charging)", level)
	}
	return fmt.Sprintf("%d%%", level)
}

// setFrontlight sets the front light: 0 is off, up to 24 is brightest.
func setFrontlight(level int) {
	if !onKindle() || level < 0 {
		return
	}
	if err := lipcSetInt("com.lab126.powerd", "flIntensity", level); err != nil {
		log.Printf("frontlight: %v", err)
	}
}

// leaveScreenSaver brings powerd back to its active state if a press on the
// power button sent it into the Kindle's own screensaver.  Left there, powerd
// would soon put the Kindle to sleep itself, with no alarm to wake it for
// the next redraw.  It reports whether the screensaver was on.
func leaveScreenSaver() bool {
	state, err := lipcGet("com.lab126.powerd", "state")
	if err != nil || !strings.EqualFold(state, "screenSaver") {
		return false
	}
	log.Printf("power: Kindle screensaver is on; returning to the dashboard")
	// The same as pressing the power button once more.
	if err := lipcSetInt("com.lab126.powerd", "powerButton", 1); err != nil {
		log.Printf("power: leaving screensaver: %v", err)
	}
	return true
}
