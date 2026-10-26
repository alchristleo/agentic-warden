package sync

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// CheckTransport refuses a plain-HTTP control plane URL unless its host is
// loopback, so a token or credential never crosses a network in the clear.
// Loopback means the host is exactly "localhost" or parses as a loopback IP;
// there is no DNS lookup, so "localhost.evil.com" is refused.
func CheckTransport(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("sync: invalid control plane URL %q: %w", rawURL, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("sync: refusing %s: plain http is only allowed to localhost; use an https URL", u.Redacted())
}

// CheckRedirect is the redirect policy for every control plane client. A
// custom CheckRedirect replaces Go's default ten-hop cap, so the cap is
// restored here before the transport check on the hop's URL.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("sync: stopped after 10 redirects")
	}
	return CheckTransport(req.URL.String())
}
