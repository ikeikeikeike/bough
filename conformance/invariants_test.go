package conformance

import "testing"

// TestExtractDialableAddrs_InfersSchemeDefaultPort is the regression
// guard for issue #71: a *_URL value with no explicit port used to be
// silently dropped, leaving AssertReachable's v0.2.6 guard blind to it.
// The scheme's default port is now inferred so the address is dialed.
func TestExtractDialableAddrs_InfersSchemeDefaultPort(t *testing.T) {
	cases := map[string]string{
		"redis://10.0.0.5/0":       "10.0.0.5:6379",
		"postgres://10.0.0.5/db":   "10.0.0.5:5432",
		"http://10.0.0.5/_cluster": "10.0.0.5:80",
		"redis://10.0.0.5:53001/0": "10.0.0.5:53001", // explicit port survives
		"postgresql://10.0.0.5/db": "10.0.0.5:5432",
	}
	for urlVal, want := range cases {
		t.Run(urlVal, func(t *testing.T) {
			got := extractDialableAddrs(map[string]string{"BOUGH_X_URL": urlVal})
			if len(got) != 1 || got[0] != want {
				t.Errorf("extractDialableAddrs(%q) = %v, want [%q]", urlVal, got, want)
			}
		})
	}
}

// TestExtractDialableAddrs_SkipsUnknownSchemeWithoutPort verifies that
// an unrecognised scheme with no port is left unverified — there is no
// sensible default to dial, so guessing one would only produce a bogus
// connection error.
func TestExtractDialableAddrs_SkipsUnknownSchemeWithoutPort(t *testing.T) {
	if got := extractDialableAddrs(map[string]string{"BOUGH_X_URL": "customproto://host/path"}); len(got) != 0 {
		t.Errorf("got %v, want [] (unknown scheme, no default port)", got)
	}
}

// TestExtractDialableAddrs_SkipsHostlessURL verifies a URL with no host
// (nothing to dial) is skipped rather than emitted as a portless addr.
func TestExtractDialableAddrs_SkipsHostlessURL(t *testing.T) {
	if got := extractDialableAddrs(map[string]string{"BOUGH_X_URL": "redis:///0"}); len(got) != 0 {
		t.Errorf("got %v, want [] (no host)", got)
	}
}
