//go:build !linux

package main

import (
	"errors"
	"time"
)

var errTooSoon = errors.New("next wake-up is too close to be worth sleeping")

// suspendUntil only works on the Kindle.
func suspendUntil(t time.Time) (early bool, err error) {
	return false, errors.New("sleeping is only supported on the Kindle")
}

func clearWakeAlarms() {}
