// fetchassets downloads the pictures listed in assets/manifest.txt (OpenMoji)
// and the display font into assets/, where they get embedded into the binary.
package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const openmojiBase = "https://raw.githubusercontent.com/hfg-gmuend/openmoji/master/color/618x618/"

// Rounded, chunky display fonts (all OFL); the first that downloads wins.
var fontURLs = []string{
	"https://raw.githubusercontent.com/google/fonts/main/ofl/lilitaone/LilitaOne-Regular.ttf",
	"https://raw.githubusercontent.com/google/fonts/main/ofl/sniglet/Sniglet-ExtraBold.ttf",
	"https://raw.githubusercontent.com/google/fonts/main/apache/chewy/Chewy-Regular.ttf",
}

var client = &http.Client{Timeout: 30 * time.Second}

func download(url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

func main() {
	force := len(os.Args) > 1 && os.Args[1] == "-force"
	picDir := filepath.Join("assets", "pics")
	if err := os.MkdirAll(picDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	f, err := os.Open(filepath.Join("assets", "manifest.txt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	failed := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		name, code := fields[0], strings.ToUpper(fields[1])
		dest := filepath.Join(picDir, name+".png")
		if _, err := os.Stat(dest); err == nil && !force {
			continue
		}
		// OpenMoji names some files with the FE0F variation selector, some without.
		err := download(openmojiBase+code+".png", dest)
		if err != nil {
			err = download(openmojiBase+code+"-FE0F.png", dest)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED %s (%s): %v\n", name, code, err)
			failed++
			continue
		}
		fmt.Println("fetched", name)
	}

	fontDest := filepath.Join("assets", "font.ttf")
	if _, err := os.Stat(fontDest); err != nil || force {
		ok := false
		for _, u := range fontURLs {
			if err := download(u, fontDest); err == nil {
				fmt.Println("fetched font", u)
				ok = true
				break
			}
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "FAILED font: none of the candidate URLs worked")
			failed++
		}
	}

	if failed > 0 {
		os.Exit(1)
	}
}
