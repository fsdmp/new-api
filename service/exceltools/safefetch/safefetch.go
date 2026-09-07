// Package safefetch implements an SSRF-hardened HTTP fetcher for the
// web_fetch server tool: scheme whitelist, DNS pinning (resolve → validate
// all IPs → dial the validated IP), private/link-local/metadata IP denial on
// every redirect hop, and size/time budgets.
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	readability "github.com/go-shiori/go-readability"
)

const DefaultUserAgent = "Mozilla/5.0 (compatible; ExcelGatewayBot/1.0)"

// Options bounds one fetch.
type Options struct {
	Timeout      time.Duration
	MaxBytes     int64
	MaxRedirects int // total hops; each hop re-validated
	UserAgent    string
}

func (o *Options) fill() {
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 5 << 20
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = 3
	}
	if o.UserAgent == "" {
		o.UserAgent = DefaultUserAgent
	}
}

// Result is the extracted page.
type Result struct {
	FinalURL  string
	Title     string
	Text      string // readability main content (plain text with paragraph breaks)
	Truncated bool
}

// ErrSSRF is returned when a URL/host/IP violates the policy.
var ErrSSRF = errors.New("blocked by SSRF policy")

// blockedIPError distinguishes policy rejections for logging.
type blockedIPError struct {
	Host string
	IP   net.IP
}

func (e *blockedIPError) Error() string {
	return fmt.Sprintf("host %s resolves to blocked address %s", e.Host, e.IP.String())
}

func (e *blockedIPError) Is(err error) bool { return err == ErrSSRF }

// newPinnedTransport returns a transport whose dialer resolves the host,
// validates every resolved IP, and dials the validated IP directly. Because
// validation and dialing share one resolution, DNS-rebinding (clean answer
// for the check, private answer for the dial) has no window.
func newPinnedTransport(timeout time.Duration) *http.Transport {
	resolver := &net.Resolver{}
	return &http.Transport{
		DisableKeepAlives: true,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// Host already an IP literal: validate directly.
			if ip := net.ParseIP(host); ip != nil {
				if common.IsIPPrivate(ip) {
					return nil, &blockedIPError{Host: host, IP: ip}
				}
				var d net.Dialer
				return d.DialContext(ctx, network, net.JoinHostPort(host, port))
			}
			// Resolve once; every answer must be clean, then dial the first.
			ips, err := resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("dns resolve %s: %w", host, err)
			}
			if err := validateResolvedIPs(host, ips); err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("dns resolve %s: no addresses", host)
			}
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
}

// validateResolvedIPs rejects a hostname if ANY resolved address is private —
// this is the DNS-rebinding defense (a clean + private mixed answer is treated
// as an attack, not as "dial the clean one").
func validateResolvedIPs(host string, ips []net.IPAddr) error {
	for _, ip := range ips {
		if common.IsIPPrivate(ip.IP) {
			return &blockedIPError{Host: host, IP: ip.IP}
		}
	}
	return nil
}

// validateScheme enforces the http/https whitelist.
func validateScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q not allowed (http/https only)", ErrSSRF, u.Scheme)
	}
	return nil
}

// Fetch retrieves a URL and extracts its main content.
func Fetch(ctx context.Context, rawURL string, opts Options) (*Result, error) {
	opts.fill()

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	current, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if err := validateScheme(current); err != nil {
		return nil, err
	}

	transport := newPinnedTransport(opts.Timeout)
	client := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		// manual redirects: every hop re-runs scheme + DNS pinning validation
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var resp *http.Response
	for hop := 0; ; hop++ {
		if hop > opts.MaxRedirects {
			return nil, fmt.Errorf("too many redirects (>%d)", opts.MaxRedirects)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", opts.UserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")

		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
		if isRedirect(resp.StatusCode) {
			location := resp.Header.Get("Location")
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)) //nolint:errcheck
			resp.Body.Close()
			if location == "" {
				return nil, fmt.Errorf("redirect without Location (status %d)", resp.StatusCode)
			}
			next, err := current.Parse(location)
			if err != nil {
				return nil, fmt.Errorf("invalid redirect target %q: %w", location, err)
			}
			if err := validateScheme(next); err != nil {
				return nil, err
			}
			current = next
			continue
		}
		break
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	truncated := int64(len(body)) > opts.MaxBytes
	if truncated {
		body = body[:opts.MaxBytes]
	}

	contentType := resp.Header.Get("Content-Type")
	return extract(current.String(), contentType, body, truncated)
}

func isRedirect(status int) bool {
	return status == http.StatusMovedPermanently || status == http.StatusFound ||
		status == http.StatusSeeOther || status == http.StatusTemporaryRedirect ||
		status == http.StatusPermanentRedirect
}

// extract pulls the readable main content from an HTML page, degrading to
// tag-stripped text when readability cannot parse it.
func extract(finalURL, contentType string, body []byte, truncated bool) (*Result, error) {
	if strings.Contains(contentType, "image/") || strings.Contains(contentType, "application/pdf") {
		return nil, fmt.Errorf("unsupported content type for text extraction: %s", contentType)
	}

	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") || contentType == "" {
		parsedURL, _ := url.Parse(finalURL)
		article, err := readability.FromReader(strings.NewReader(string(body)), parsedURL)
		if err == nil && strings.TrimSpace(article.TextContent) != "" {
			text := article.TextContent
			title := article.Title
			if title == "" {
				title = finalURL
			}
			return &Result{FinalURL: finalURL, Title: title, Text: text, Truncated: truncated}, nil
		}
		// fall through to naive extraction
	}

	title, text := naiveStripHTML(body)
	return &Result{FinalURL: finalURL, Title: title, Text: text, Truncated: truncated}, nil
}

// naiveStripHTML is the fallback extractor: removes script/style blocks and
// tags, collapses whitespace. Good enough for the model to read the content.
func naiveStripHTML(body []byte) (title, text string) {
	s := string(body)
	if start := strings.Index(strings.ToLower(s), "<title>"); start >= 0 {
		if end := strings.Index(strings.ToLower(s[start:]), "</title>"); end > 0 {
			title = strings.TrimSpace(s[start+7 : start+end])
		}
	}
	lower := strings.ToLower(s)
	for {
		i := strings.Index(lower, "<script")
		if i < 0 {
			break
		}
		j := strings.Index(lower[i:], "</script>")
		if j < 0 {
			s = s[:i]
			lower = strings.ToLower(s)
			break
		}
		s = s[:i] + s[i+j+9:]
		lower = strings.ToLower(s)
	}
	for {
		i := strings.Index(lower, "<style")
		if i < 0 {
			break
		}
		j := strings.Index(lower[i:], "</style>")
		if j < 0 {
			s = s[:i]
			lower = strings.ToLower(s)
			break
		}
		s = s[:i] + s[i+j+8:]
		lower = strings.ToLower(s)
	}
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	text = strings.Join(strings.Fields(b.String()), " ")
	if title == "" {
		title = text
		if len([]rune(title)) > 80 {
			title = string([]rune(title)[:80])
		}
	}
	return title, text
}
