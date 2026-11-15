//go:build linux

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

// evdev ioctl numbers (generic _IOC encoding, as used on ARM).
const (
	evKey = 0x01
	evAbs = 0x03

	absPressure    = 0x18
	absMTPositionX = 0x35
	btnTouch       = 0x14a

	eviocgrab = 0x40044590 // _IOW('E', 0x90, int)
)

// eviocgbit builds EVIOCGBIT(ev, len): read the capability bits of one event type.
func eviocgbit(ev, length uintptr) uintptr {
	return 2<<30 | length<<16 | 'E'<<8 | (0x20 + ev)
}

func eviocgname(length uintptr) uintptr {
	return 2<<30 | length<<16 | 'E'<<8 | 0x06
}

func ioctl(fd, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); errno != 0 {
		return errno
	}
	return nil
}

func capBits(f *os.File, ev uintptr) []byte {
	buf := make([]byte, 96) // 768 bits: covers every key code
	if ioctl(f.Fd(), eviocgbit(ev, uintptr(len(buf))), uintptr(unsafe.Pointer(&buf[0]))) != nil {
		return nil
	}
	return buf
}

func hasBit(bits []byte, n int) bool {
	return n/8 < len(bits) && bits[n/8]&(1<<(n%8)) != 0
}

// startInput takes over the Kindle's input devices.
//
// With the lock on, the touchscreen is grabbed exclusively, so neither the
// Kindle's own interface nor anything else sees a single tap.  The page-turn
// buttons are grabbed too and their presses are delivered on the returned
// channel.  The grabs vanish automatically when this process exits, however
// it exits, so a crash can never leave the Kindle unusable.
//
// The returned orientation channel carries the Kindle's motion sensor: how
// many quarter turns clockwise the Kindle now is from upright (0-3).  It is
// only read when display.orientation is "auto".
func startInput(cfg *Config) (<-chan uint16, <-chan int, func()) {
	keys := make(chan uint16, 64)
	orient := make(chan int, 8)
	var held []*os.File

	for i := 0; i < 32; i++ {
		path := fmt.Sprintf("/dev/input/event%d", i)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		nameBuf := make([]byte, 80)
		ioctl(f.Fd(), eviocgname(uintptr(len(nameBuf))), uintptr(unsafe.Pointer(&nameBuf[0])))
		name := string(nameBuf[:clen(nameBuf)])

		keyBits, absBits := capBits(f, evKey), capBits(f, evAbs)
		isTouch := hasBit(absBits, absMTPositionX) || hasBit(keyBits, btnTouch)
		hasPower := hasBit(keyBits, keyPower)
		hasPage := hasBit(keyBits, keyPageUp) || hasBit(keyBits, keyPageDown) ||
			hasBit(keyBits, keyLPgBack) || hasBit(keyBits, keyLPgFwd)

		// The Oasis accelerometer reports its position as EV_ABS:ABS_PRESSURE
		// (see KOReader's frontend/device/kindle/device.lua).
		isSensor := !isTouch && hasBit(absBits, absPressure)
		if isSensor && cfg.autoRotate() {
			log.Printf("input: %s %q motion sensor", path, name)
			held = append(held, f)
			go readSensor(f, orient)
			continue
		}

		grab, read := false, false
		switch {
		case isTouch:
			grab = cfg.Lock.Enabled
		case hasPage:
			read = true
			// Leave a combined page+power device ungrabbed unless asked, so
			// the power button keeps working.
			grab = cfg.Lock.Enabled && (!hasPower || cfg.Lock.GrabPowerButton)
		case hasPower:
			grab = cfg.Lock.Enabled && cfg.Lock.GrabPowerButton
			read = grab
		}
		if !grab && !read {
			f.Close()
			continue
		}
		if grab {
			one := 1
			if err := ioctl(f.Fd(), eviocgrab, uintptr(one)); err != nil {
				log.Printf("input: cannot grab %s (%s): %v", path, name, err)
				grab = false
			}
		}
		log.Printf("input: %s %q touch=%v page=%v power=%v grabbed=%v", path, name, isTouch, hasPage, hasPower, grab)
		held = append(held, f)
		if read {
			go readKeys(f, keys)
		}
	}
	if len(held) == 0 {
		log.Printf("input: no usable input devices found")
	}

	release := func() {
		for _, f := range held {
			ioctl(f.Fd(), eviocgrab, 0)
			f.Close()
		}
	}
	return keys, orient, release
}

// readKeys forwards key-down events from one device.
func readKeys(f *os.File, keys chan<- uint16) {
	// struct input_event: a timeval, then type, code (u16) and value (s32).
	evSize := int(unsafe.Sizeof(syscall.Timeval{})) + 8
	buf := make([]byte, evSize*16)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+evSize <= n; off += evSize {
			ev := buf[off+evSize-8:]
			typ := binary.LittleEndian.Uint16(ev[0:])
			code := binary.LittleEndian.Uint16(ev[2:])
			value := int32(binary.LittleEndian.Uint32(ev[4:]))
			if typ == evKey && value == 1 {
				select {
				case keys <- code:
				default: // nobody listening fast enough; drop it
				}
			}
		}
	}
}

// Oasis 2/3 accelerometer positions (drivers/input/misc/accel/bma2x2.c), as
// clockwise quarter turns from upright.
var sensorTurns = map[int32]int{
	15: 0, // portrait, upright
	17: 1, // landscape: turned clockwise
	16: 2, // portrait, upside down
	18: 3, // landscape: turned counter-clockwise
}

// readSensor forwards position changes from the motion sensor.
func readSensor(f *os.File, orient chan<- int) {
	evSize := int(unsafe.Sizeof(syscall.Timeval{})) + 8
	buf := make([]byte, evSize*16)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+evSize <= n; off += evSize {
			ev := buf[off+evSize-8:]
			typ := binary.LittleEndian.Uint16(ev[0:])
			code := binary.LittleEndian.Uint16(ev[2:])
			value := int32(binary.LittleEndian.Uint32(ev[4:]))
			if t, ok := sensorTurns[value]; ok && typ == evAbs && code == absPressure {
				select {
				case orient <- t:
				default:
				}
			}
		}
	}
}

func clen(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return len(b)
}
