package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

// Display pushes finished frames to the e-ink screen.  It prefers FBInk when
// one is installed (better refresh control) and falls back to the stock eips.
type Display struct {
	fbink            string // path to fbink, or "" to use eips
	nativeW, nativeH int    // the screen as fbink/eips address it
	W, H             int    // page size as the viewer sees it
	turns            int    // clockwise quarter turns applied to each frame: 0-3
	tmp              string
}

// setTurns sets how far each frame is turned on its way to the screen, and
// with it the page size the viewer sees.
func (d *Display) setTurns(t int) {
	d.turns = ((t % 4) + 4) % 4
	if d.turns%2 == 1 {
		d.W, d.H = d.nativeH, d.nativeW
	} else {
		d.W, d.H = d.nativeW, d.nativeH
	}
}

func openDisplay(cfg *Config) *Display {
	d := &Display{W: cfg.Display.Width, H: cfg.Display.Height}

	candidates := []string{
		cfg.path("bin/fbink"),
		"/mnt/us/libkh/bin/fbink",
		"/mnt/us/usbnet/bin/fbink",
		"/mnt/us/koreader/fbink",
	}
	if p, err := exec.LookPath("fbink"); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			d.fbink = p
			break
		}
	}

	if (d.W == 0 || d.H == 0) && d.fbink != "" {
		// "fbink -e" prints viewWidth=1264;viewHeight=1680;...
		if out, err := exec.Command(d.fbink, "-e").Output(); err == nil {
			if m := regexp.MustCompile(`viewWidth=(\d+)`).FindSubmatch(out); m != nil {
				d.W, _ = strconv.Atoi(string(m[1]))
			}
			if m := regexp.MustCompile(`viewHeight=(\d+)`).FindSubmatch(out); m != nil {
				d.H, _ = strconv.Atoi(string(m[1]))
			}
		}
	}
	if d.W == 0 || d.H == 0 {
		d.W, d.H = 1264, 1680 // Kindle Oasis 2 / 3
	}

	// The screen itself stays as the Kindle left it (normally upright).  When
	// the dashboard wants the other orientation, frames are drawn sideways and
	// turned a quarter on their way out; from here on W and H describe the
	// page as the child sees it.  With "auto", the motion sensor sets the turn
	// later (see App.run).
	d.nativeW, d.nativeH = d.W, d.H
	d.setTurns(0)
	if !cfg.autoRotate() && (d.W > d.H) != cfg.landscape() {
		if cfg.Display.Orientation == "landscape-flipped" {
			d.setTurns(3)
		} else {
			d.setTurns(1)
		}
	}

	dir := "/var/tmp"
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = os.TempDir()
	}
	d.tmp = filepath.Join(dir, "kidsdash.png")

	if d.fbink != "" {
		log.Printf("display: %dx%d via %s", d.W, d.H, d.fbink)
	} else {
		log.Printf("display: %dx%d via eips", d.W, d.H)
	}
	return d
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Show draws the frame.  full asks for a flashing refresh, which clears the
// ghosting that quiet updates slowly build up.
func (d *Display) Show(img *image.Gray, full bool) error {
	switch d.turns {
	case 1:
		img = rotate90(img, true)
	case 2:
		img = rotate180(img)
	case 3:
		img = rotate90(img, false)
	}
	if err := writePNG(d.tmp, img); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if d.fbink != "" {
		// -w: wait until the screen has finished changing, so the Kindle
		// is never put to sleep halfway through drawing.
		args := []string{"-q", "-w", "-g", "file=" + d.tmp}
		if full {
			args = append(args, "-f")
		}
		cmd = exec.Command(d.fbink, args...)
	} else {
		args := []string{"-g", d.tmp}
		if full {
			args = append([]string{"-f"}, args...)
		}
		cmd = exec.Command("eips", args...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", cmd.Path, err, out)
	}
	return nil
}
