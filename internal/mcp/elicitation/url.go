package elicitation

import (
	"net/url"
	"strings"
)

// URLTarget is the destination of a URL-mode elicitation as the user must
// see it before consenting: the full URL, its host, and whether the host
// carries a punycode label.
type URLTarget struct {
	// URL is the server-supplied URL, verbatim.
	URL string
	// Host is the lower-cased hostname without port, or "" when the URL
	// does not parse or names no host.
	Host string
	// Punycode reports an "xn--" label in Host. Punycode can spell a
	// lookalike of a trusted domain, so the spec asks clients to warn.
	Punycode bool
}

// ParseURLTarget extracts the host a URL-mode elicitation points at. It
// never fetches the URL.
func ParseURLTarget(raw string) URLTarget {
	target := URLTarget{URL: raw}
	u, err := url.Parse(raw)
	if err != nil {
		return target
	}
	target.Host = strings.ToLower(u.Hostname())
	for _, label := range strings.Split(target.Host, ".") {
		if strings.HasPrefix(label, "xn--") {
			target.Punycode = true
		}
	}
	return target
}

// PunycodeWarning is shown next to a URL whose host has a punycode label.
const PunycodeWarning = "Warning: the host contains punycode (xn--); check it is the site you expect."

// URLNotice renders a URL-mode elicitation for a user who opens the URL
// by hand: the server's message, the full URL, its host, and a punycode
// warning when one applies.
func URLNotice(message, rawURL string) string {
	target := ParseURLTarget(rawURL)
	var b strings.Builder
	if message != "" {
		b.WriteString(message)
		b.WriteString("\n")
	}
	b.WriteString("URL:  ")
	b.WriteString(target.URL)
	b.WriteString("\nHost: ")
	if target.Host == "" {
		b.WriteString("(unparseable URL)")
	} else {
		b.WriteString(target.Host)
	}
	if target.Punycode {
		b.WriteString("\n")
		b.WriteString(PunycodeWarning)
	}
	return b.String()
}
