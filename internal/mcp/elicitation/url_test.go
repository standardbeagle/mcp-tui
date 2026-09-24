package elicitation

import (
	"strings"
	"testing"
)

// TestURLNotice_ShowsMessageFullURLAndHost: the spec requires the full URL
// before consent and recommends highlighting its host against subdomain
// spoofing.
func TestURLNotice_ShowsMessageFullURLAndHost(t *testing.T) {
	const target = "https://sso.example.com/device?code=WDJB-MJHT"
	notice := URLNotice("Sign in to continue", target)
	for _, want := range []string{"Sign in to continue", target, "Host: sso.example.com"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice lacks %q:\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "punycode") {
		t.Errorf("ASCII host flagged as punycode:\n%s", notice)
	}
}

// TestURLNotice_WarnsOnPunycodeHost: a punycode label can spell a lookalike
// of a trusted domain, which the spec says clients should warn about.
func TestURLNotice_WarnsOnPunycodeHost(t *testing.T) {
	notice := URLNotice("Pay invoice", "https://xn--pypal-4ve.com/checkout")
	if !strings.Contains(notice, "punycode") {
		t.Errorf("punycode host not flagged:\n%s", notice)
	}
}

// TestURLNotice_UnparseableURL names the problem instead of inventing a host.
func TestURLNotice_UnparseableURL(t *testing.T) {
	notice := URLNotice("Sign in", "https://sso.example.com:bad port/")
	if !strings.Contains(notice, "Host: (unparseable URL)") {
		t.Errorf("unparseable URL not reported:\n%s", notice)
	}
}

func TestParseURLTarget(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		host     string
		punycode bool
	}{
		{"https://sso.example.com/device", "sso.example.com", false},
		{"https://login.XN--80ak6aa92e.com:8443/x", "login.xn--80ak6aa92e.com", true},
		{"not a url", "", false},
	} {
		got := ParseURLTarget(tc.raw)
		if got.Host != tc.host || got.Punycode != tc.punycode || got.URL != tc.raw {
			t.Errorf("ParseURLTarget(%q) = %+v, want host %q punycode %v", tc.raw, got, tc.host, tc.punycode)
		}
	}
}
