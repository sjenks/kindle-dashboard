package main

import (
	"strconv"
	"time"
)

// Linux key codes of the Kindle's physical buttons.
const (
	keyPageUp   = 104 // Oasis page-turn button
	keyPageDown = 109 // Oasis page-turn button
	keyPower    = 116
	keyLPgBack  = 193 // page-turn buttons on older Kindles
	keyLPgFwd   = 191
)

// keyName gives a button the name used in unlock_sequence.
func keyName(code uint16) string {
	switch code {
	case keyPageUp, keyLPgBack:
		return "prev"
	case keyPageDown, keyLPgFwd:
		return "next"
	case keyPower:
		return "power"
	}
	return strconv.Itoa(int(code))
}

// normalizeKey accepts the friendlier spellings people put in the config.
func normalizeKey(s string) string {
	switch s {
	case "prev", "previous", "back", "up":
		return "prev"
	case "next", "forward", "fwd", "down":
		return "next"
	}
	return s
}

// seqMatcher recognises the parent's unlock sequence.
//
// To stop a button-mashing child from stumbling onto it, an attempt only
// starts after a quiet pause, and a single wrong press spoils the attempt
// until the next pause.
type seqMatcher struct {
	seq     []string
	timeout time.Duration // the whole sequence must be entered within this
	quiet   time.Duration // pause that starts a fresh attempt

	idx     int
	spoiled bool
	last    time.Time
	started time.Time
}

func newSeqMatcher(seq []string, timeout time.Duration) *seqMatcher {
	m := &seqMatcher{timeout: timeout, quiet: 2 * time.Second}
	for _, s := range seq {
		m.seq = append(m.seq, normalizeKey(s))
	}
	return m
}

// press feeds one button press; it returns true when the sequence completes.
func (m *seqMatcher) press(code uint16, now time.Time) bool {
	if m.last.IsZero() || now.Sub(m.last) >= m.quiet {
		m.idx, m.spoiled, m.started = 0, false, now
	}
	m.last = now
	if m.spoiled || len(m.seq) == 0 {
		return false
	}
	want := m.seq[m.idx]
	if want != keyName(code) && want != strconv.Itoa(int(code)) {
		m.spoiled = true
		return false
	}
	m.idx++
	if m.idx < len(m.seq) {
		return false
	}
	m.spoiled = true // a completed or too-slow attempt cannot be extended
	return now.Sub(m.started) <= m.timeout
}
