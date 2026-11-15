package main

import (
	"image"
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
