package main

import (
	"strconv"
	"strings"
	"time"
)

// A deliberately small iCalendar reader: enough for the family calendars that
// Google, iCloud and Outlook publish.  It understands single and all-day
// events, multi-day events, and DAILY / WEEKLY / MONTHLY / YEARLY repeats with
// INTERVAL, COUNT, UNTIL, BYDAY, BYMONTHDAY and EXDATE.

type icsProp struct {
	params map[string]string
	value  string
}

type icsEvent struct {
	uid       string
	summary   string
	start     time.Time
	end       time.Time
	allDay    bool
	rrule     map[string]string
	exdates   map[string]bool
	override  bool // has RECURRENCE-ID: replaces one instance of a series
	recurID   time.Time
	cancelled bool
}

func parseICS(data []byte, loc *time.Location, from, to time.Time) []Event {
	// Unfold: a line starting with space or tab continues the previous one.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")

	var events []*icsEvent
	var cur *icsEvent
	depth := 0 // nesting inside VEVENT (e.g. VALARM), whose properties we ignore
	for _, line := range strings.Split(text, "\n") {
		name, prop, ok := parseICSLine(line)
		if !ok {
			continue
		}
		switch {
		case name == "BEGIN" && prop.value == "VEVENT":
			cur = &icsEvent{exdates: map[string]bool{}}
			depth = 0
		case name == "BEGIN" && cur != nil:
			depth++
		case name == "END" && prop.value == "VEVENT":
			if cur != nil && !cur.start.IsZero() {
				events = append(events, cur)
			}
			cur = nil
		case name == "END" && cur != nil:
			depth--
		case cur == nil || depth > 0:
			// outside an event, or inside a nested component
		case name == "UID":
			cur.uid = prop.value
		case name == "SUMMARY":
			cur.summary = icsUnescape(prop.value)
		case name == "STATUS":
			cur.cancelled = prop.value == "CANCELLED"
		case name == "DTSTART":
			cur.start, cur.allDay = parseICSTime(prop, loc)
		case name == "DTEND":
			cur.end, _ = parseICSTime(prop, loc)
		case name == "RRULE":
			cur.rrule = map[string]string{}
			for _, part := range strings.Split(prop.value, ";") {
				if k, v, ok := strings.Cut(part, "="); ok {
					cur.rrule[k] = v
				}
			}
		case name == "EXDATE":
			for _, v := range strings.Split(prop.value, ",") {
				if t, _ := parseICSTime(icsProp{prop.params, v}, loc); !t.IsZero() {
					cur.exdates[t.Format("20060102T1504")] = true
				}
			}
		case name == "RECURRENCE-ID":
			cur.recurID, _ = parseICSTime(prop, loc)
			cur.override = true
		}
	}

	// An override replaces one instance of its series: exclude that instance.
	byUID := map[string]*icsEvent{}
	for _, e := range events {
		if !e.override && e.rrule != nil {
			byUID[e.uid] = e
		}
	}
	for _, e := range events {
		if e.override && !e.recurID.IsZero() {
			if master := byUID[e.uid]; master != nil {
				master.exdates[e.recurID.Format("20060102T1504")] = true
			}
		}
	}

	var out []Event
	for _, e := range events {
		if e.cancelled || e.summary == "" {
			continue
		}
		var starts []time.Time
		if e.rrule != nil && !e.override {
			starts = expandRRule(e, from, to)
		} else if !e.start.Before(from) && !e.start.After(to) {
			starts = []time.Time{e.start}
		} else if e.start.Before(from) && e.end.After(from) {
			// Already under way (e.g. a week-long trip): show it as today.
			starts = []time.Time{from}
			e.allDay = true
		}
		for _, s := range starts {
			out = append(out, Event{When: s, AllDay: e.allDay, Title: e.summary})
		}
	}
	return out
}

// parseICSLine splits "NAME;PARAM=X:value".
func parseICSLine(line string) (string, icsProp, bool) {
	// The first colon outside double quotes ends the name/params part.
	colon, quoted := -1, false
	for i := 0; i < len(line); i++ {
		if line[i] == '"' {
			quoted = !quoted
		} else if line[i] == ':' && !quoted {
			colon = i
			break
		}
	}
	if colon < 0 {
		return "", icsProp{}, false
	}
	parts := strings.Split(line[:colon], ";")
	prop := icsProp{params: map[string]string{}, value: strings.TrimSpace(line[colon+1:])}
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			prop.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return strings.ToUpper(parts[0]), prop, true
}

func parseICSTime(p icsProp, loc *time.Location) (t time.Time, allDay bool) {
	v := p.value
	if len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, loc)
		return t, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		if t, err := time.Parse("20060102T150405Z", v); err == nil {
			return t.In(loc), false
		}
		return time.Time{}, false
	}
	zone := loc
	if tzid := p.params["TZID"]; tzid != "" {
		// Unknown names (Outlook's "Eastern Standard Time") fall back to ours.
		if l, err := time.LoadLocation(tzid); err == nil {
			zone = l
		}
	}
	if t, err := time.ParseInLocation("20060102T150405", v, zone); err == nil {
		return t.In(loc), false
	}
	return time.Time{}, false
}

func icsUnescape(s string) string {
	return strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

var icsWeekdays = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// expandRRule lists the starts of a repeating event that fall within [from, to].
func expandRRule(e *icsEvent, from, to time.Time) []time.Time {
	loc := e.start.Location()
	interval, _ := strconv.Atoi(e.rrule["INTERVAL"])
	if interval < 1 {
		interval = 1
	}
	count, _ := strconv.Atoi(e.rrule["COUNT"])
	until := to
	if u := e.rrule["UNTIL"]; u != "" {
		if t, allDay := parseICSTime(icsProp{value: u}, loc); !t.IsZero() {
			if allDay {
				t = t.AddDate(0, 0, 1).Add(-time.Second)
			}
			if t.Before(until) {
				until = t
			}
		}
	}

	// BYDAY entries: a weekday with an optional ordinal ("TU", "2TU", "-1FR").
	type byDay struct {
		day time.Weekday
		nth int
	}
	var byDays []byDay
	for _, d := range strings.Split(e.rrule["BYDAY"], ",") {
		if len(d) < 2 {
			continue
		}
		wd, ok := icsWeekdays[d[len(d)-2:]]
		if !ok {
			continue
		}
		nth, _ := strconv.Atoi(d[:len(d)-2])
		byDays = append(byDays, byDay{wd, nth})
	}
	var monthDays []int
	for _, d := range strings.Split(e.rrule["BYMONTHDAY"], ",") {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			monthDays = append(monthDays, n)
		}
	}

	y, m, d := e.start.Date()
	hh, mm, ss := e.start.Clock()
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, hh, mm, ss, 0, loc)
	}

	// candidates returns the occurrences in the i-th period, in order.
	var candidates func(i int) []time.Time
	switch e.rrule["FREQ"] {
	case "DAILY":
		candidates = func(i int) []time.Time { return []time.Time{at(y, m, d+i*interval)} }
	case "WEEKLY":
		monday := d - (int(e.start.Weekday())+6)%7
		candidates = func(i int) []time.Time {
			if len(byDays) == 0 {
				return []time.Time{at(y, m, d+7*i*interval)}
			}
			var ts []time.Time
			for off := 0; off < 7; off++ {
				t := at(y, m, monday+7*i*interval+off)
				for _, b := range byDays {
					if b.day == t.Weekday() {
						ts = append(ts, t)
						break
					}
				}
			}
			return ts
		}
	case "MONTHLY":
		candidates = func(i int) []time.Time {
			first := at(y, m+time.Month(i*interval), 1)
			var ts []time.Time
			switch {
			case len(byDays) > 0:
				for _, b := range byDays {
					if t, ok := nthWeekday(first, b.day, b.nth); ok {
						ts = append(ts, t)
					}
				}
			case len(monthDays) > 0:
				for _, md := range monthDays {
					if t := first.AddDate(0, 0, md-1); t.Month() == first.Month() {
						ts = append(ts, t)
					}
				}
			default:
				if t := first.AddDate(0, 0, d-1); t.Month() == first.Month() {
					ts = append(ts, t)
				}
			}
			return ts
		}
	case "YEARLY":
		candidates = func(i int) []time.Time {
			t := at(y+i*interval, m, d)
			if t.Month() != m { // Feb 29 in a non-leap year
				return nil
			}
			return []time.Time{t}
		}
	default:
		return nil
	}

	var out []time.Time
	seen := 0
	// Periods can be empty (the 31st, Feb 29); the cap bounds the walk from a
	// start date that may be years back.
	for i := 0; i < 20000; i++ {
		done := false
		for _, t := range candidates(i) {
			if t.Before(e.start) {
				continue
			}
			if t.After(until) {
				done = true
				break
			}
			seen++
			if count > 0 && seen > count {
				done = true
				break
			}
			if !t.Before(from) && !e.exdates[t.Format("20060102T1504")] {
				out = append(out, t)
			}
		}
		if done {
			break
		}
	}
	return out
}

// nthWeekday finds e.g. the 2nd Tuesday (nth=2) or last Friday (nth=-1) of the
// month that starts at first.  nth=0 is treated as the first.
func nthWeekday(first time.Time, day time.Weekday, nth int) (time.Time, bool) {
	if nth >= 0 {
		if nth == 0 {
			nth = 1
		}
		offset := (int(day) - int(first.Weekday()) + 7) % 7
		t := first.AddDate(0, 0, offset+7*(nth-1))
		return t, t.Month() == first.Month()
	}
	last := first.AddDate(0, 1, -1)
	offset := (int(last.Weekday()) - int(day) + 7) % 7
	t := last.AddDate(0, 0, -offset+7*(nth+1))
	return t, t.Month() == first.Month()
}
