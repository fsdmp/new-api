package safefetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSchemeWhitelist(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:70",
		"ftp://example.com/file",
		"data:text/html,hello",
		"javascript:alert(1)",
	} {
		_, err := Fetch(context.Background(), raw, Options{})
		if !errors.Is(err, ErrSSRF) {
			t.Errorf("Fetch(%q) err = %v, want ErrSSRF", raw, err)
		}
	}
}

func TestPrivateIPLiteralBlocked(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:8080/",
		"http://localhost:8080/", // resolves to loopback
		"http://10.0.0.1/",
		"http://172.16.5.4/",
		"http://192.168.1.1/admin",
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://100.64.0.1/",                       // CGNAT
		"http://0.0.0.0/",
		"http://[::1]:8080/",
		"http://[fe80::1]/",
		"http://[fc00::1]/",
	}
	for _, raw := range cases {
		_, err := Fetch(context.Background(), raw, Options{Timeout: 2 * time.Second})
		if !errors.Is(err, ErrSSRF) {
			t.Errorf("Fetch(%q) err = %v, want ErrSSRF", raw, err)
		}
	}
}

// The loopback HTTP server is itself a private target — an end-to-end proof
// that the pinned dialer enforces policy before any connect.
func TestLoopbackServerBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), srv.URL, Options{Timeout: 2 * time.Second})
	if !errors.Is(err, ErrSSRF) {
		t.Errorf("Fetch(loopback server) err = %v, want ErrSSRF", err)
	}
}

// Any private answer in a mixed DNS reply rejects the whole host (rebinding
// defense: no "dial the clean one").
func TestValidateResolvedIPsMixedAnswers(t *testing.T) {
	mustAddr := func(s string) net.IPAddr {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad ip %q", s)
		}
		return net.IPAddr{IP: ip}
	}
	cases := []struct {
		ips   []net.IPAddr
		bad   bool
	}{
		{[]net.IPAddr{mustAddr("1.1.1.1"), mustAddr("8.8.8.8")}, false},
		{[]net.IPAddr{mustAddr("8.8.8.8"), mustAddr("10.0.0.1")}, true},
		{[]net.IPAddr{mustAddr("93.184.216.34"), mustAddr("169.254.169.254")}, true},
		{[]net.IPAddr{mustAddr("2606:4700:4700::1111")}, false},
		{[]net.IPAddr{mustAddr("2606:4700:4700::1111"), mustAddr("fd00::1")}, true},
	}
	for _, tc := range cases {
		err := validateResolvedIPs("host.test", tc.ips)
		if tc.bad && !errors.Is(err, ErrSSRF) {
			t.Errorf("validateResolvedIPs(%v) err = %v, want ErrSSRF", tc.ips, err)
		}
		if !tc.bad && err != nil {
			t.Errorf("validateResolvedIPs(%v) err = %v, want nil", tc.ips, err)
		}
	}
}

func TestExtractContentGates(t *testing.T) {
	if _, err := extract("http://x", "application/pdf", []byte("%PDF"), false); err == nil {
		t.Error("pdf must be rejected")
	}
	if _, err := extract("http://x", "image/png", []byte("png"), false); err == nil {
		t.Error("images must be rejected")
	}
}

func TestNaiveStripHTML(t *testing.T) {
	html := `<html><head><title>财报 - 示例站</title><style>body{color:red}</style>
	<script>alert("x")</script></head><body><h1>第一章</h1><p>营收增长 12%。</p></body></html>`
	title, text := naiveStripHTML([]byte(html))
	if title != "财报 - 示例站" {
		t.Errorf("title = %q", title)
	}
	if strings.Contains(text, "alert") || strings.Contains(text, "color:red") {
		t.Errorf("script/style leaked: %q", text)
	}
	if !strings.Contains(text, "营收增长 12%") || !strings.Contains(text, "第一章") {
		t.Errorf("text content lost: %q", text)
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{}
	o.fill()
	if o.Timeout != 15*time.Second || o.MaxBytes != 5<<20 || o.MaxRedirects != 3 || o.UserAgent == "" {
		t.Errorf("defaults wrong: %+v", o)
	}
}
