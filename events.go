package main

import (
	"crypto/sha1"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Event struct {
	When    time.Time // start; midnight for all-day events
	AllDay  bool
	Title   string
	Picture string // picture name; empty means "pick one from the title"
}

// localEvent is one entry of events.yaml.
type localEvent struct {
	Date    string `yaml:"date"` // YYYY-MM-DD, or MM-DD for every year
	Time    string `yaml:"time"` // optional HH:MM
	Title   string `yaml:"title"`
	Picture string `yaml:"picture"`
}

// keywordPictures maps words in an event title to a picture.  First match wins.
var keywordPictures = []struct{ word, pic string }{
	{"birthday", "cake"}, {"bday", "cake"}, {"party", "party"},
	{"dentist", "dentist"}, {"tooth", "dentist"}, {"orthodont", "dentist"},
	{"doctor", "doctor"}, {"pediatric", "doctor"}, {"checkup", "doctor"}, {"check-up", "doctor"}, {"vaccin", "doctor"},
	{"hospital", "hospital"},
	{"swim", "swim"}, {"pool", "swim"},
	{"soccer", "soccer"}, {"football", "soccer"}, {"basketball", "basketball"}, {"baseball", "baseball"}, {"t-ball", "baseball"},
	{"ballet", "ballet"}, {"dance", "ballet"}, {"gymnastic", "gymnastics"}, {"tumbl", "gymnastics"},
	{"piano", "piano"}, {"guitar", "guitar"}, {"music", "music"}, {"choir", "music"}, {"concert", "music"}, {"recital", "music"},
	{"art", "art"}, {"paint", "art"}, {"craft", "art"},
	{"library", "books"}, {"book", "books"}, {"story", "books"},
	{"movie", "movie"}, {"cinema", "movie"}, {"film", "movie"},
	{"playground", "playground"}, {"park", "playground"}, {"playdate", "teddy"}, {"play date", "teddy"},
	{"zoo", "zoo"}, {"beach", "beach"}, {"camp", "camping"}, {"farm", "farm"}, {"circus", "circus"},
	{"grandma", "grandma"}, {"granny", "grandma"}, {"nana", "grandma"},
	{"grandpa", "grandpa"}, {"grandad", "grandpa"}, {"granddad", "grandpa"},
	{"family", "family"}, {"cousin", "family"}, {"baby", "baby"},
	{"flight", "plane"}, {"fly ", "plane"}, {"airport", "plane"}, {"plane", "plane"},
	{"vacation", "beach"}, {"holiday", "beach"}, {"trip", "car"}, {"drive", "car"},
	{"train", "train"}, {"bus", "bus"}, {"bike", "bike"}, {"cycling", "bike"},
	{"christmas", "christmas-tree"}, {"xmas", "christmas-tree"}, {"santa", "santa"},
	{"halloween", "pumpkin"}, {"pumpkin", "pumpkin"}, {"trick or treat", "pumpkin"},
	{"easter", "bunny"}, {"thanksgiving", "turkey"},
	{"new year", "fireworks"}, {"firework", "fireworks"}, {"4th of july", "fireworks"}, {"bonfire", "fireworks"},
	{"valentine", "heart"}, {"mother's day", "heart"}, {"father's day", "heart"}, {"wedding", "heart"},
	{"haircut", "scissors"}, {"hair cut", "scissors"}, {"barber", "scissors"},
	{"pizza", "pizza"}, {"ice cream", "ice-cream"}, {"picnic", "picnic"},
	{"vet", "dog"}, {"dog", "dog"}, {"puppy", "dog"}, {"cat", "cat"}, {"kitten", "cat"},
	{"church", "church"}, {"shopping", "shopping"}, {"groceries", "shopping"},
	{"sled", "sled"}, {"ski", "sled"}, {"bath", "bath"},
	{"school", "school"}, {"preschool", "school"}, {"kindergarten", "school"}, {"nursery", "school"}, {"daycare", "school"},
	{"present", "gift"}, {"gift", "gift"},
}

// pictureFor chooses the picture for an event.
func pictureFor(e Event, cfg *Config) string {
	if e.Picture != "" {
		return e.Picture
	}
	title := strings.ToLower(e.Title)
	for word, pic := range cfg.Keywords {
		if hasWord(title, strings.ToLower(word)) {
			return pic
		}
	}
	for _, k := range keywordPictures {
		if hasWord(title, k.word) {
			return k.pic
		}
	}
	return "calendar"
}

// hasWord reports whether word appears in title at the start of a word, so
// "art" matches "art class" but not "party".
func hasWord(title, word string) bool {
	for i := 0; ; {
		j := strings.Index(title[i:], word)
		if j < 0 {
			return false
		}
		j += i
		if j == 0 || !isLetter(title[j-1]) {
			return true
		}
		i = j + 1
	}
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// loadLocalEvents reads events.yaml and returns occurrences within [from, to].
func loadLocalEvents(cfg *Config, from, to time.Time) ([]Event, error) {
	data, err := os.ReadFile(cfg.path(cfg.EventsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file struct {
		Events []localEvent `yaml:"events"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.EventsFile, err)
	}

	var out []Event
	for _, le := range file.Events {
		var dates []time.Time
		if d, err := time.ParseInLocation("2006-01-02", le.Date, cfg.loc); err == nil {
			dates = append(dates, d)
		} else if d, err := time.ParseInLocation("01-02", le.Date, cfg.loc); err == nil {
			// Yearly: try this year and next so December still sees January.
			for y := from.Year(); y <= to.Year(); y++ {
				dates = append(dates, time.Date(y, d.Month(), d.Day(), 0, 0, 0, 0, cfg.loc))
			}
		} else {
			log.Printf("events: skipping %q: bad date %q (want YYYY-MM-DD or MM-DD)", le.Title, le.Date)
			continue
		}
		for _, d := range dates {
			e := Event{When: d, AllDay: true, Title: le.Title, Picture: le.Picture}
			if le.Time != "" {
				if t, err := time.Parse("15:04", le.Time); err == nil {
					e.When = time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, cfg.loc)
					e.AllDay = false
				}
			}
			if !e.When.Before(from) && !e.When.After(to) {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

func icsCacheName(url string) string {
	sum := sha1.Sum([]byte(url))
	return fmt.Sprintf("cal-%x.ics", sum[:8])
}

// fetchCalendars downloads every configured ICS feed into the cache and
// reports whether all of them succeeded.
func fetchCalendars(cfg *Config) bool {
	ok := true
	for _, url := range cfg.Calendars {
		// Calendar apps hand out webcal:// links; they are plain https.
		fetchURL := strings.Replace(url, "webcal://", "https://", 1)
		data, err := httpGet(fetchURL)
		if err != nil {
			// The URL is a secret (it grants access to the calendar): keep it out of the log.
			log.Printf("calendar fetch failed: %v", scrubURL(err, fetchURL))
			ok = false
			continue
		}
		if !strings.Contains(string(data), "BEGIN:VCALENDAR") {
			log.Printf("calendar fetch: response is not an ICS feed")
			ok = false
			continue
		}
		os.WriteFile(cfg.cachePath(icsCacheName(url)), data, 0o644)
	}
	return ok
}

func scrubURL(err error, url string) string {
	return strings.ReplaceAll(err.Error(), url, "<calendar url>")
}

// loadEvents merges events.yaml with the cached calendar feeds and returns
// what is coming up from today onwards, soonest first.
func loadEvents(cfg *Config, now time.Time) []Event {
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cfg.loc)
	to := from.AddDate(0, 0, cfg.DaysAhead+1)

	events, err := loadLocalEvents(cfg, from, to)
	if err != nil {
		log.Printf("events: %v", err)
	}
	for _, url := range cfg.Calendars {
		data, err := os.ReadFile(cfg.cachePath(icsCacheName(url)))
		if err != nil {
			continue
		}
		events = append(events, parseICS(data, cfg.loc, from, to)...)
	}

	sort.SliceStable(events, func(i, j int) bool { return events[i].When.Before(events[j].When) })

	// Drop duplicates (same day, same title), preferring the one with a picture.
	var out []Event
	seen := map[string]int{}
	for _, e := range events {
		key := e.When.Format("2006-01-02") + "|" + strings.ToLower(strings.TrimSpace(e.Title))
		if i, ok := seen[key]; ok {
			if out[i].Picture == "" && e.Picture != "" {
				out[i].Picture = e.Picture
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, e)
	}
	return out
}
