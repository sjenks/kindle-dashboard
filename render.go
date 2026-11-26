package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

//go:embed assets/pics/*.png
var picFS embed.FS

//go:embed assets/font.ttf
var fontData []byte

// The layout is designed on a 1264-wide page (Kindle Oasis 2/3) and scaled to
// whatever the screen really is.
const designW, designH = 1264.0, 1680.0

const (
	black = 0x00
	dark  = 0x3a
	mid   = 0x80
	light = 0xd8
	white = 0xff
)

const (
	alignLeft = iota
	alignCenter
	alignRight
)

// Scene is everything one screen shows.
type Scene struct {
	Now     time.Time
	Weather *Weather
	Events  []Event
	Battery *Battery // nil off the Kindle
}

type Canvas struct {
	img *image.RGBA
	s   float64 // device pixels per design unit
	cfg *Config

	dw, dh float64 // page size in design units
}

var (
	parsedFont *opentype.Font
	faceCache  = map[int]font.Face{}
	picCache   = map[string]*image.RGBA{}
	renderMu   sync.Mutex
	gammaLUT   [256]uint8
)

func init() {
	f, err := opentype.Parse(fontData)
	if err != nil {
		panic("embedded font: " + err.Error())
	}
	parsedFont = f
	// E-ink washes out light tones (a yellow sun turns nearly white), so
	// pictures get their mid-tones pulled darker.
	for i := range gammaLUT {
		gammaLUT[i] = uint8(math.Round(255 * math.Pow(float64(i)/255, 1.5)))
	}
}

func gray(v uint8) *image.Uniform { return image.NewUniform(color.RGBA{v, v, v, 0xff}) }

// ---- drawing primitives (all coordinates in design units) ----

type pathBuilder struct {
	z      *vector.Rasterizer
	s      float64
	ox, oy float64
}

func (p pathBuilder) move(x, y float64) {
	p.z.MoveTo(float32(x*p.s-p.ox), float32(y*p.s-p.oy))
}
func (p pathBuilder) line(x, y float64) {
	p.z.LineTo(float32(x*p.s-p.ox), float32(y*p.s-p.oy))
}
func (p pathBuilder) cube(x1, y1, x2, y2, x3, y3 float64) {
	p.z.CubeTo(float32(x1*p.s-p.ox), float32(y1*p.s-p.oy),
		float32(x2*p.s-p.ox), float32(y2*p.s-p.oy),
		float32(x3*p.s-p.ox), float32(y3*p.s-p.oy))
}

// shape fills a path, rasterizing only its bounding box.
func (c *Canvas) shape(x0, y0, x1, y1 float64, col uint8, build func(p pathBuilder)) {
	r := image.Rect(int(math.Floor(x0*c.s))-1, int(math.Floor(y0*c.s))-1,
		int(math.Ceil(x1*c.s))+1, int(math.Ceil(y1*c.s))+1).Intersect(c.img.Bounds())
	if r.Empty() {
		return
	}
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	build(pathBuilder{z, c.s, float64(r.Min.X), float64(r.Min.Y)})
	z.Draw(c.img, r, gray(col), image.Point{})
}

func (c *Canvas) rrect(x, y, w, h, r float64, col uint8) {
	r = math.Min(r, math.Min(w, h)/2)
	k := r * 0.5523 // bezier handle length for a quarter circle
	c.shape(x, y, x+w, y+h, col, func(p pathBuilder) {
		p.move(x+r, y)
		p.line(x+w-r, y)
		p.cube(x+w-r+k, y, x+w, y+r-k, x+w, y+r)
		p.line(x+w, y+h-r)
		p.cube(x+w, y+h-r+k, x+w-r+k, y+h, x+w-r, y+h)
		p.line(x+r, y+h)
		p.cube(x+r-k, y+h, x, y+h-r+k, x, y+h-r)
		p.line(x, y+r)
		p.cube(x, y+r-k, x+r-k, y, x+r, y)
		p.z.ClosePath()
	})
}

func (c *Canvas) circle(cx, cy, r float64, col uint8) {
	c.rrect(cx-r, cy-r, 2*r, 2*r, r, col)
}

// panel is a white rounded box with a black outline.
func (c *Canvas) panel(x, y, w, h, border float64) {
	const radius = 40
	c.rrect(x, y, w, h, radius, black)
	c.rrect(x+border, y+border, w-2*border, h-2*border, radius-border, white)
}

// stroke draws a thick line with round ends.
func (c *Canvas) stroke(x1, y1, x2, y2, width float64, col uint8) {
	dx, dy := x2-x1, y2-y1
	n := math.Hypot(dx, dy)
	if n == 0 {
		return
	}
	nx, ny := -dy/n*width/2, dx/n*width/2
	pad := width
	c.shape(math.Min(x1, x2)-pad, math.Min(y1, y2)-pad, math.Max(x1, x2)+pad, math.Max(y1, y2)+pad, col,
		func(p pathBuilder) {
			p.move(x1+nx, y1+ny)
			p.line(x2+nx, y2+ny)
			p.line(x2-nx, y2-ny)
			p.line(x1-nx, y1-ny)
			p.z.ClosePath()
		})
	c.circle(x1, y1, width/2, col)
	c.circle(x2, y2, width/2, col)
}

func face(px int) font.Face {
	if px < 4 {
		px = 4
	}
	if f, ok := faceCache[px]; ok {
		return f
	}
	f, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	faceCache[px] = f
	return f
}

// textWidth measures s at the given size, in design units.
func (c *Canvas) textWidth(s string, size float64) float64 {
	return float64(font.MeasureString(face(int(math.Round(size*c.s))), s)) / 64 / c.s
}

// fitSize shrinks size until s fits in maxW.
func (c *Canvas) fitSize(s string, size, maxW float64) float64 {
	if w := c.textWidth(s, size); w > maxW {
		size = math.Floor(size * maxW / w)
	}
	return size
}

// text draws s with its baseline at y.
func (c *Canvas) text(s string, x, y, size float64, col uint8, align int) {
	f := face(int(math.Round(size * c.s)))
	w := font.MeasureString(f, s)
	px := fixed.Int26_6(x * c.s * 64)
	switch align {
	case alignCenter:
		px -= w / 2
	case alignRight:
		px -= w
	}
	d := font.Drawer{Dst: c.img, Src: gray(col), Face: f, Dot: fixed.Point26_6{X: px, Y: fixed.Int26_6(y * c.s * 64)}}
	d.DrawString(s)
}

// wrap splits s into at most maxLines lines no wider than maxW.
func (c *Canvas) wrap(s string, size, maxW float64, maxLines int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		try := word
		if cur != "" {
			try = cur + " " + word
		}
		if cur == "" || c.textWidth(try, size) <= maxW {
			cur = try
			continue
		}
		lines = append(lines, cur)
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] += "..."
	}
	// Trim any line that is still too wide (one very long word, or the "...").
	for i, l := range lines {
		for c.textWidth(l, size) > maxW && len(l) > 4 {
			r := []rune(strings.TrimSuffix(l, "..."))
			l = strings.TrimSpace(string(r[:len(r)-1])) + "..."
		}
		lines[i] = l
	}
	return lines
}

// ---- pictures ----

// openPicture finds a picture by name: first in the user's pictures folder
// (so a photo of grandma can replace the drawing), then among the built-ins.
func openPicture(cfg *Config, name string) (img image.Image, builtin bool) {
	if cfg != nil && name != "" {
		for _, ext := range []string{"", ".png", ".jpg", ".jpeg"} {
			if data, err := os.ReadFile(cfg.path("pictures/" + name + ext)); err == nil {
				if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					return img, false
				}
				log.Printf("picture %s%s: cannot decode", name, ext)
			}
		}
	}
	data, err := picFS.ReadFile("assets/pics/" + name + ".png")
	if err != nil {
		return nil, false
	}
	img, _, err = image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	return img, true
}

// scaledPicture returns the named picture as a grayscale size x size tile.
func scaledPicture(cfg *Config, name string, size int) *image.RGBA {
	key := fmt.Sprintf("%s@%d", name, size)
	if t, ok := picCache[key]; ok {
		return t
	}
	src, builtin := openPicture(cfg, name)
	if src == nil {
		log.Printf("picture %q not found", name)
		if name == "calendar" {
			return nil
		}
		return scaledPicture(cfg, "calendar", size)
	}
	sb := src.Bounds()
	if builtin {
		// The built-in drawings carry a transparent margin; crop most of it.
		inset := sb.Dx() * 7 / 100
		sb = sb.Inset(inset)
	}
	scale := math.Min(float64(size)/float64(sb.Dx()), float64(size)/float64(sb.Dy()))
	dw, dh := int(float64(sb.Dx())*scale), int(float64(sb.Dy())*scale)
	ox, oy := (size-dw)/2, (size-dh)/2

	tile := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(tile, image.Rect(ox, oy, ox+dw, oy+dh), src, sb, xdraw.Src, nil)

	// Convert to gray (premultiplied alpha stays as is).
	for i := 0; i < len(tile.Pix); i += 4 {
		a := int(tile.Pix[i+3])
		if a == 0 {
			continue
		}
		r, g, b := int(tile.Pix[i])*255/a, int(tile.Pix[i+1])*255/a, int(tile.Pix[i+2])*255/a
		l := (299*r + 587*g + 114*b) / 1000
		if l > 255 {
			l = 255
		}
		v := uint8(int(gammaLUT[l]) * a / 255)
		tile.Pix[i], tile.Pix[i+1], tile.Pix[i+2] = v, v, v
	}
	picCache[key] = tile
	return tile
}

// pic draws the named picture in a size x size box with its top-left at x, y.
func (c *Canvas) pic(name string, x, y, size float64) {
	px := int(math.Round(size * c.s))
	tile := scaledPicture(c.cfg, name, px)
	if tile == nil {
		return
	}
	at := image.Pt(int(math.Round(x*c.s)), int(math.Round(y*c.s)))
	draw.Draw(c.img, image.Rectangle{at, at.Add(image.Pt(px, px))}, tile, image.Point{}, draw.Over)
}

// ---- the dashboard ----

// newCanvas makes a blank white page of w x h pixels.  A page wider than it
// is tall gets the landscape design space (the portrait one turned sideways).
func newCanvas(cfg *Config, w, h int) *Canvas {
	c := &Canvas{img: image.NewRGBA(image.Rect(0, 0, w, h)), cfg: cfg, dw: designW, dh: designH}
	if w > h {
		c.dw, c.dh = designH, designW
	}
	c.s = math.Min(float64(w)/c.dw, float64(h)/c.dh)
	draw.Draw(c.img, c.img.Bounds(), gray(white), image.Point{}, draw.Src)
	return c
}

func render(cfg *Config, sc Scene, w, h int) *image.Gray {
	renderMu.Lock()
	defer renderMu.Unlock()

	c := newCanvas(cfg, w, h)
	const margin = 44.0
	inner := c.dw - 2*margin

	if w > h {
		// Landscape: clock, weather and forecast side by side, events below.
		c.drawHeaderWide(sc, margin, inner)
		c.drawWeek(sc, margin, 196, inner, 84)
		c.drawClock(sc, margin, 304, 576, 520)
		c.drawWeather(sc, margin+600, 304, 576, 520)
		c.drawForecastTall(sc, margin+1200, 304, inner-1200, 520)
		c.drawEventsWide(sc, margin, 848, inner, 372)
		c.drawBattery(sc, margin+inner, 1252)
	} else {
		c.drawHeader(sc, margin, inner)
		c.drawWeek(sc, margin, 318, inner, 96)
		c.drawClock(sc, margin, 446, 576, 520)
		c.drawWeather(sc, margin+600, 446, 576, 520)
		c.drawForecast(sc, margin, 990, inner, 200)
		c.drawEvents(sc, margin, 1214, inner, 426)
		c.drawBattery(sc, margin+inner, 1668)
	}
	return toGray(c.img)
}

// rotate90 turns a frame a quarter turn, clockwise or counter-clockwise.
func rotate90(src *image.Gray, clockwise bool) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride : y*src.Stride+w]
		for x, v := range row {
			if clockwise {
				dst.Pix[x*dst.Stride+(h-1-y)] = v
			} else {
				dst.Pix[(w-1-x)*dst.Stride+y] = v
			}
		}
	}
	return dst
}

// rotate180 turns a frame upside down.
func rotate180(src *image.Gray) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride : y*src.Stride+w]
		out := dst.Pix[(h-1-y)*dst.Stride:]
		for x, v := range row {
			out[w-1-x] = v
		}
	}
	return dst
}

func toGray(src *image.RGBA) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(b)
	for i := range dst.Pix {
		dst.Pix[i] = src.Pix[i*4]
	}
	return dst
}

func seasonOf(t time.Time, latitude float64) (pic, label string) {
	m := int(t.Month())
	if latitude < 0 {
		m = (m+5)%12 + 1 // shift half a year for the southern hemisphere
	}
	switch {
	case m >= 3 && m <= 5:
		return "spring", "Spring"
	case m >= 6 && m <= 8:
		return "summer", "Summer"
	case m >= 9 && m <= 11:
		return "autumn", "Autumn"
	}
	return "winter", "Winter"
}

func partOfDay(t time.Time) (pic, label string) {
	switch h := t.Hour(); {
	case h >= 6 && h < 12:
		return "morning", "Morning"
	case h >= 12 && h < 17:
		return "afternoon", "Afternoon"
	case h >= 17 && h < 20:
		return "evening", "Evening"
	}
	return "night", "Night time"
}

func (c *Canvas) drawHeader(sc Scene, x, w float64) {
	const seasonSize = 200.0
	day := sc.Now.Weekday().String()
	c.text(day, x, 172, c.fitSize(day, 176, w-seasonSize-30), black, alignLeft)
	c.text(sc.Now.Format("January 2"), x+4, 268, 78, dark, alignLeft)

	pic, label := seasonOf(sc.Now, c.cfg.Location.Latitude)
	sx := x + w - seasonSize
	c.pic(pic, sx, 20, seasonSize)
	c.text(label, sx+seasonSize/2, 268, 50, dark, alignCenter)
}

// drawHeaderWide is the landscape header: day, date and season on one line.
func (c *Canvas) drawHeaderWide(sc Scene, x, w float64) {
	const seasonSize, base = 164.0, 150.0
	day := sc.Now.Weekday().String()
	size := c.fitSize(day, 150, 700)
	c.text(day, x, base, size, black, alignLeft)
	c.text(sc.Now.Format("January 2"), x+c.textWidth(day, size)+36, base, 72, dark, alignLeft)

	pic, label := seasonOf(sc.Now, c.cfg.Location.Latitude)
	sx := x + w - seasonSize
	c.pic(pic, sx, 14, seasonSize)
	c.text(label, sx-14, base, 50, dark, alignRight)
}

// drawBattery is a small line for the grown-ups along the bottom edge,
// ending at right: a battery with the level, and roughly how long is left.
func (c *Canvas) drawBattery(sc Scene, right, base float64) {
	b := sc.Battery
	if b == nil {
		return
	}
	const size, bw, bh = 28.0, 40.0, 20.0
	label := batteryLabel(*b)
	c.text(label, right, base, size, mid, alignRight)

	// The battery: an outline with a nub, filled as far as it is charged.
	x := right - c.textWidth(label, size) - 14 - bw
	y := base - bh
	c.rrect(x+bw, y+6, 4, bh-12, 1, mid)
	c.rrect(x, y, bw, bh, 4, mid)
	c.rrect(x+3, y+3, bw-6, bh-6, 2, white)
	fill := (bw - 10) * math.Max(0, math.Min(100, float64(b.Level))) / 100
	if fill > 0 {
		c.rrect(x+5, y+5, fill, bh-10, 1, mid)
	}
}

// drawWeek shows the seven days of this week with today highlighted.
func (c *Canvas) drawWeek(sc Scene, x, y, w, h float64) {
	const gap = 14.0
	base := h/2 + 18 // text baseline within a pill
	pw := (w - 6*gap) / 7
	first := time.Sunday
	if c.cfg.WeekStart == "monday" {
		first = time.Monday
	}
	today := (int(sc.Now.Weekday()) - int(first) + 7) % 7
	for i := 0; i < 7; i++ {
		px := x + float64(i)*(pw+gap)
		name := time.Weekday((int(first) + i) % 7).String()[:3]
		switch {
		case i < today:
			c.rrect(px, y, pw, h, h/2, light)
			c.text(name, px+pw/2, y+base, 50, mid, alignCenter)
		case i == today:
			c.rrect(px, y, pw, h, h/2, black)
			c.text(name, px+pw/2, y+base, 50, white, alignCenter)
		default:
			c.rrect(px, y, pw, h, h/2, black)
			c.rrect(px+5, y+5, pw-10, h-10, h/2-5, white)
			c.text(name, px+pw/2, y+base, 50, black, alignCenter)
		}
	}
}

func formatClock(t time.Time, clock string) string {
	if clock == "24h" {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

func (c *Canvas) drawClock(sc Scene, x, y, w, h float64) {
	c.panel(x, y, w, h, 6)

	cx, cy, r := x+w/2, y+200.0, 176.0
	c.circle(cx, cy, r, black)
	c.circle(cx, cy, r-9, white)
	for n := 1; n <= 12; n++ {
		a := float64(n) * math.Pi / 6
		sin, cos := math.Sin(a), math.Cos(a)
		c.stroke(cx+(r-26)*sin, cy-(r-26)*cos, cx+(r-18)*sin, cy-(r-18)*cos, 6, black)
		c.text(fmt.Sprint(n), cx+(r-62)*sin, cy-(r-62)*cos+15, 44, black, alignCenter)
	}
	hour, min := sc.Now.Hour()%12, sc.Now.Minute()
	ha := (float64(hour) + float64(min)/60) * math.Pi / 6
	ma := float64(min) * math.Pi / 30
	c.stroke(cx, cy, cx+78*math.Sin(ha), cy-78*math.Cos(ha), 18, black)
	c.stroke(cx, cy, cx+120*math.Sin(ma), cy-120*math.Cos(ma), 10, dark)
	c.circle(cx, cy, 15, black)

	pic, label := partOfDay(sc.Now)
	c.pic(pic, x+40, y+h-136, 108)
	c.text(formatClock(sc.Now, c.cfg.Clock), x+168, y+h-76, 62, black, alignLeft)
	c.text(label, x+168, y+h-32, 40, dark, alignLeft)
}

func degrees(v float64) string { return fmt.Sprintf("%.0f°", math.Round(v)+0) }

func (c *Canvas) drawWeather(sc Scene, x, y, w, h float64) {
	c.panel(x, y, w, h, 6)
	wx := sc.Weather
	var today *DayForecast
	if wx != nil {
		today = wx.day(sc.Now)
	}
	if wx == nil || (today == nil && !wx.currentFresh(sc.Now)) {
		c.pic("cloud", x+w/2-110, y+90, 220)
		c.text("Weather is loading", x+w/2, y+390, 46, mid, alignCenter)
		return
	}

	// Prefer the live reading; fall back to today's forecast when it is stale.
	isDay := sc.Now.Hour() >= 7 && sc.Now.Hour() < 19
	var code, precip int
	var temp, feels float64
	if wx.currentFresh(sc.Now) {
		code, temp, feels, isDay = wx.Code, wx.Temp, wx.Feels, wx.IsDay
	} else {
		code, temp, feels = today.Code, today.Hi, today.Hi
	}
	if today != nil {
		precip = today.PrecipProb
	}

	pic, label := weatherLook(code, isDay)
	c.pic(pic, x+14, y+12, 290)

	const right = 38.0
	t := degrees(temp)
	c.text(t, x+w-right, y+178, c.fitSize(t, 170, w-290-right-10), black, alignRight)
	if today != nil {
		c.text(degrees(today.Hi)+" / "+degrees(today.Lo), x+w-right, y+246, 46, dark, alignRight)
	}
	c.text(label, x+w/2, y+352, c.fitSize(label, 58, w-60), black, alignCenter)

	feelsC := feels
	if c.cfg.Units == "fahrenheit" {
		feelsC = (feels - 32) / 1.8
	}
	wear := clothing(feelsC, code, precip, isDay)
	const size, gap = 124.0, 22.0
	total := float64(len(wear))*size + float64(len(wear)-1)*gap
	for i, name := range wear {
		c.pic(name, x+(w-total)/2+float64(i)*(size+gap), y+h-size-16, size)
	}
}

func (c *Canvas) drawForecast(sc Scene, x, y, w, h float64) {
	c.panel(x, y, w, h, 6)
	const cols = 4
	cw := (w - 24) / cols
	for i := 0; i < cols; i++ {
		date := sc.Now.AddDate(0, 0, i+1)
		cx := x + 12 + float64(i)*cw
		if i > 0 {
			c.rrect(cx-2, y+30, 4, h-60, 2, light)
		}
		name := date.Weekday().String()[:3]
		var f *DayForecast
		if sc.Weather != nil {
			f = sc.Weather.day(date)
		}
		if f == nil {
			c.text(name, cx+cw/2, y+h/2+16, 46, mid, alignCenter)
			continue
		}
		pic, _ := weatherLook(f.Code, true)
		c.pic(pic, cx+8, y+(h-114)/2, 114)
		tx := cx + 134
		c.text(name, tx, y+84, 46, black, alignLeft)
		hi := degrees(f.Hi)
		c.text(hi, tx, y+142, 44, black, alignLeft)
		c.text(degrees(f.Lo), tx+c.textWidth(hi, 44)+12, y+142, 36, mid, alignLeft)
	}
}

// drawForecastTall is the landscape forecast: the next four days stacked.
func (c *Canvas) drawForecastTall(sc Scene, x, y, w, h float64) {
	c.panel(x, y, w, h, 6)
	const rows = 4
	rh := (h - 24) / rows
	for i := 0; i < rows; i++ {
		date := sc.Now.AddDate(0, 0, i+1)
		ry := y + 12 + float64(i)*rh
		if i > 0 {
			c.rrect(x+30, ry-2, w-60, 4, 2, light)
		}
		name := date.Weekday().String()[:3]
		var f *DayForecast
		if sc.Weather != nil {
			f = sc.Weather.day(date)
		}
		if f == nil {
			c.text(name, x+w/2, ry+rh/2+16, 46, mid, alignCenter)
			continue
		}
		pic, _ := weatherLook(f.Code, true)
		c.pic(pic, x+26, ry+(rh-108)/2, 108)
		tx := x + 158
		c.text(name, tx, ry+54, 46, black, alignLeft)
		hi := degrees(f.Hi)
		c.text(hi, tx, ry+104, 44, black, alignLeft)
		c.text(degrees(f.Lo), tx+c.textWidth(hi, 44)+12, ry+104, 36, mid, alignLeft)
	}
}

// drawEventsWide is the landscape "coming up" row: picture left, words right.
func (c *Canvas) drawEventsWide(sc Scene, x, y, w, h float64) {
	const maxCards, gap = 3, 24.0
	events := sc.Events
	if len(events) > maxCards {
		events = events[:maxCards]
	}
	if len(events) == 0 {
		c.panel(x, y, w, h, 6)
		c.pic("teddy", x+w/2-520, y+(h-250)/2, 250)
		c.text("Nothing planned. Time to play!", x+w/2+120, y+h/2+18, 54, dark, alignCenter)
		return
	}

	cw := (w - gap*(maxCards-1)) / maxCards
	total := float64(len(events))*cw + float64(len(events)-1)*gap
	for i, e := range events {
		cx := x + (w-total)/2 + float64(i)*(cw+gap)
		label := countdown(e, sc.Now, c.cfg.Clock)
		isToday := strings.HasPrefix(label, "Today")
		border := 6.0
		if isToday {
			border = 12
		}
		c.panel(cx, y, cw, h, border)

		const picSize = 216.0
		c.pic(pictureFor(e, c.cfg), cx+24, y+(h-picSize)/2, picSize)

		// Title and countdown chip, centred as a block beside the picture.
		const lineH, chipH, chipGap = 46.0, 60.0, 22.0
		tw := cw - picSize - 24 - 44
		tcx := cx + 24 + picSize + 12 + tw/2
		lines := c.wrap(e.Title, 40, tw, 3)
		top := y + (h-(float64(len(lines))*lineH+chipGap+chipH))/2
		for j, l := range lines {
			c.text(l, tcx, top+36+float64(j)*lineH, 40, black, alignCenter)
		}

		size := c.fitSize(label, 38, tw-48)
		chipW := c.textWidth(label, size) + 48
		chipY := top + float64(len(lines))*lineH + chipGap
		fill, ink := uint8(light), uint8(black)
		if isToday {
			fill, ink = black, white
		}
		c.rrect(tcx-chipW/2, chipY, chipW, chipH, chipH/2, fill)
		c.text(label, tcx, chipY+43, size, ink, alignCenter)
	}
}

// countdown says how far away an event is, the way a small child counts.
func countdown(e Event, now time.Time, clock string) string {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(e.When.Year(), e.When.Month(), e.When.Day(), 0, 0, 0, 0, now.Location())
	switch n := int(math.Round(day.Sub(today).Hours() / 24)); {
	case n <= 0 && e.AllDay:
		return "Today!"
	case n <= 0:
		return "Today " + formatClock(e.When, clock)
	case n == 1:
		return "Tomorrow"
	default:
		return fmt.Sprintf("%d sleeps", n)
	}
}

func (c *Canvas) drawEvents(sc Scene, x, y, w, h float64) {
	const maxCards, gap = 3, 24.0
	events := sc.Events
	if len(events) > maxCards {
		events = events[:maxCards]
	}
	if len(events) == 0 {
		c.panel(x, y, w, h, 6)
		c.pic("teddy", x+w/2-130, y+40, 260)
		c.text("Nothing planned. Time to play!", x+w/2, y+h-52, 54, dark, alignCenter)
		return
	}

	cw := (w - gap*(maxCards-1)) / maxCards
	total := float64(len(events))*cw + float64(len(events)-1)*gap
	for i, e := range events {
		cx := x + (w-total)/2 + float64(i)*(cw+gap)
		label := countdown(e, sc.Now, c.cfg.Clock)
		isToday := strings.HasPrefix(label, "Today")
		border := 6.0
		if isToday {
			border = 12
		}
		c.panel(cx, y, cw, h, border)

		const picSize = 204.0
		c.pic(pictureFor(e, c.cfg), cx+(cw-picSize)/2, y+18, picSize)

		lines := c.wrap(e.Title, 42, cw-44, 2)
		base := y + 270.0
		if len(lines) == 1 {
			base += 22
		}
		for j, l := range lines {
			c.text(l, cx+cw/2, base+float64(j)*46, 42, black, alignCenter)
		}

		const chipH = 64.0
		size := c.fitSize(label, 40, cw-90)
		chipW := c.textWidth(label, size) + 56
		chipY := y + h - chipH - 22
		if isToday {
			c.rrect(cx+(cw-chipW)/2, chipY, chipW, chipH, chipH/2, black)
			c.text(label, cx+cw/2, chipY+46, size, white, alignCenter)
		} else {
			c.rrect(cx+(cw-chipW)/2, chipY, chipW, chipH, chipH/2, light)
			c.text(label, cx+cw/2, chipY+46, size, black, alignCenter)
		}
	}
}

// renderMessage draws a simple full-screen notice: a picture and a few lines.
func renderMessage(cfg *Config, w, h int, pic string, lines ...string) *image.Gray {
	renderMu.Lock()
	defer renderMu.Unlock()

	c := newCanvas(cfg, w, h)
	top := c.dh * 0.2
	if pic != "" {
		c.pic(pic, c.dw/2-180, c.dh*0.18, 360)
		top = c.dh*0.18 + 500
	}
	for i, l := range lines {
		size := 72.0
		if i > 0 {
			size = 50
		}
		c.text(l, c.dw/2, top+float64(i)*84, c.fitSize(l, size, c.dw-120), black, alignCenter)
	}
	return toGray(c.img)
}
