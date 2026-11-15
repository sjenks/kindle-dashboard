package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

type DayForecast struct {
	Date       time.Time
	Code       int
	Hi, Lo     float64
	PrecipProb int
}

type Weather struct {
	FetchedAt time.Time
	Temp      float64 // display units
	Feels     float64
	Code      int // WMO weather code
	IsDay     bool
	Days      []DayForecast
}

// openMeteo mirrors the parts of the Open-Meteo response we ask for.
type openMeteo struct {
	FetchedAt int64 `json:"fetched_at"` // added by us when caching
	Current   struct {
		Temp  float64 `json:"temperature_2m"`
		Feels float64 `json:"apparent_temperature"`
		Code  int     `json:"weather_code"`
		IsDay int     `json:"is_day"`
	} `json:"current"`
	Daily struct {
		Time   []string  `json:"time"`
		Code   []int     `json:"weather_code"`
		Hi     []float64 `json:"temperature_2m_max"`
		Lo     []float64 `json:"temperature_2m_min"`
		Precip []*int    `json:"precipitation_probability_max"`
	} `json:"daily"`
}

var httpClient = &http.Client{Timeout: 25 * time.Second}

func httpGet(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func fetchWeather(cfg *Config) (*Weather, error) {
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
		"&current=temperature_2m,apparent_temperature,weather_code,is_day"+
		"&daily=weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"+
		"&temperature_unit=%s&timezone=%s&forecast_days=6",
		cfg.Location.Latitude, cfg.Location.Longitude, cfg.Units, url.QueryEscape(cfg.Timezone))
	data, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	var om openMeteo
	if err := json.Unmarshal(data, &om); err != nil {
		return nil, err
	}
	if len(om.Daily.Time) == 0 {
		return nil, fmt.Errorf("weather response has no forecast")
	}
	om.FetchedAt = time.Now().Unix()
	if cached, err := json.Marshal(om); err == nil {
		os.WriteFile(cfg.cachePath("weather.json"), cached, 0o644)
	}
	return om.toWeather(cfg.loc), nil
}

func loadCachedWeather(cfg *Config) *Weather {
	data, err := os.ReadFile(cfg.cachePath("weather.json"))
	if err != nil {
		return nil
	}
	var om openMeteo
	if json.Unmarshal(data, &om) != nil || len(om.Daily.Time) == 0 {
		return nil
	}
	return om.toWeather(cfg.loc)
}

func (om *openMeteo) toWeather(loc *time.Location) *Weather {
	w := &Weather{
		FetchedAt: time.Unix(om.FetchedAt, 0),
		Temp:      om.Current.Temp,
		Feels:     om.Current.Feels,
		Code:      om.Current.Code,
		IsDay:     om.Current.IsDay != 0,
	}
	d := om.Daily
	for i, t := range d.Time {
		date, err := time.ParseInLocation("2006-01-02", t, loc)
		if err != nil || i >= len(d.Code) || i >= len(d.Hi) || i >= len(d.Lo) {
			continue
		}
		f := DayForecast{Date: date, Code: d.Code[i], Hi: d.Hi[i], Lo: d.Lo[i]}
		if i < len(d.Precip) && d.Precip[i] != nil {
			f.PrecipProb = *d.Precip[i]
		}
		w.Days = append(w.Days, f)
	}
	return w
}

// day returns the forecast for the given calendar date, if we have one.
func (w *Weather) day(date time.Time) *DayForecast {
	for i := range w.Days {
		if sameDay(w.Days[i].Date, date) {
			return &w.Days[i]
		}
	}
	return nil
}

// currentFresh reports whether the "right now" reading is recent enough to show.
func (w *Weather) currentFresh(now time.Time) bool {
	return now.Sub(w.FetchedAt) < 2*time.Hour
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// weatherLook maps a WMO weather code to a picture name and a short label.
func weatherLook(code int, isDay bool) (pic, label string) {
	switch {
	case code == 0:
		if isDay {
			return "sun", "Sunny"
		}
		return "moon", "Clear"
	case code == 1:
		if isDay {
			return "sun-small-cloud", "Mostly sunny"
		}
		return "moon", "Mostly clear"
	case code == 2:
		if isDay {
			return "sun-cloud", "Partly cloudy"
		}
		return "cloud", "Partly cloudy"
	case code == 3:
		return "cloud", "Cloudy"
	case code == 45 || code == 48:
		return "fog", "Foggy"
	case code >= 51 && code <= 57:
		return "rain", "Drizzly"
	case code >= 61 && code <= 67:
		return "rain", "Rainy"
	case (code >= 71 && code <= 77) || code == 85 || code == 86:
		return "snow", "Snowy"
	case code >= 80 && code <= 82:
		if isDay {
			return "sun-rain", "Showers"
		}
		return "rain", "Showers"
	case code >= 95:
		return "thunder", "Stormy"
	}
	return "cloud", "Cloudy"
}

// clothing picks up to three "what to wear" pictures.  feelsC is in Celsius.
func clothing(feelsC float64, code int, precipProb int, isDay bool) []string {
	rainy := (code >= 51 && code <= 67) || (code >= 80 && code <= 82) || code >= 95 || precipProb >= 60
	snowy := (code >= 71 && code <= 77) || code == 85 || code == 86

	var out []string
	switch {
	case snowy || feelsC < 3:
		out = []string{"coat", "scarf", "gloves"}
	case feelsC < 10:
		out = []string{"coat", "scarf"}
	case feelsC < 17:
		out = []string{"coat"}
	case feelsC < 24:
		out = []string{"tshirt"}
	default:
		out = []string{"tshirt", "shorts"}
		if isDay && code <= 2 {
			out = append(out, "sunglasses")
		}
	}
	if snowy {
		out[2] = "boots"
	} else if rainy {
		if len(out) == 3 {
			out = out[:2]
		}
		out = append(out, "umbrella")
	}
	return out
}
