//go:build !linux

package main

// startInput is only meaningful on the Kindle; elsewhere there is nothing to lock.
func startInput(cfg *Config) (<-chan uint16, func()) {
	return nil, func() {}
}
