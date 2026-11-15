package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

// nettestHost is the weather service; the test tries reaching it a few ways
// to find out which one a particular Kindle and network can manage.
const nettestHost = "api.open-meteo.com"

type nettestCase struct {
	name string
	url  string
	tls  *tls.Config
}

func nettestCases() []nettestCase {
	path := "/v1/forecast?latitude=47.6&longitude=-122.3&current=temperature_2m"
	return []nettestCase{
		{"https default", "https://" + nettestHost + path, &tls.Config{}},
		// Without the post-quantum key share, the TLS ClientHello fits in one packet.
		{"https classic", "https://" + nettestHost + path, &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256}}},
		{"https tls1.2", "https://" + nettestHost + path, &tls.Config{MaxVersion: tls.VersionTLS12}},
		{"http plain", "http://" + nettestHost + path, nil},
	}
}

// nettestOne makes one request and describes how it went.
func nettestOne(c nettestCase) string {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSHandshakeTimeout = 20 * time.Second
	if c.tls != nil {
		tr.TLSClientConfig = c.tls
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: tr}
	start := time.Now()
	resp, err := client.Get(c.url)
	took := time.Since(start).Round(100 * time.Millisecond)
	if err != nil {
		return fmt.Sprintf("FAIL %v (%v)", took, shortErr(err))
	}
	resp.Body.Close()
	return fmt.Sprintf("ok %s %v", resp.Status[:3], took)
}

func shortErr(err error) string {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "timeout"
	}
	s := err.Error()
	if len(s) > 60 {
		s = s[len(s)-60:]
	}
	return s
}

// runNettest tries each way of reaching the weather service, shows the
// results on screen as they come in, and logs them.
func runNettest(cfg *Config, disp *Display) {
	lines := []string{"Network test", "Weather service: " + nettestHost}
	show := func(full bool) {
		if err := disp.Show(renderMessage(cfg, disp.W, disp.H, "", lines...), full); err != nil {
			log.Printf("display: %v", err)
		}
	}
	addrs, err := net.LookupHost(nettestHost)
	lines = append(lines, fmt.Sprintf("address: %v %v", addrs, errOrEmpty(err)))
	log.Printf("nettest: %s", lines[len(lines)-1])
	show(true)
	// First the way the dashboard itself fetches (classic TLS, DNS retries).
	start := time.Now()
	_, err = httpGet(nettestCases()[0].url)
	r := fmt.Sprintf("ok %v", time.Since(start).Round(100*time.Millisecond))
	if err != nil {
		r = fmt.Sprintf("FAIL %v (%v)", time.Since(start).Round(100*time.Millisecond), shortErr(err))
	}
	log.Printf("nettest: dashboard: %s", r)
	lines = append(lines, "dashboard: "+r)
	show(false)
	for _, c := range nettestCases() {
		r := nettestOne(c)
		log.Printf("nettest: %s: %s", c.name, r)
		lines = append(lines, c.name+": "+r)
		show(false)
	}
	lines = append(lines, "This screen closes in 60 seconds.")
	show(false)
	time.Sleep(60 * time.Second)
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
