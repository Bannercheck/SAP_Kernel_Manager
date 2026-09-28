package platform

import "testing"

func TestParsePosixTZ(t *testing.T) {
	for _, tc := range []struct {
		in   string
		name string
		off  int
		ok   bool
	}{
		{"TRT-3", "TRT", 3 * 3600, true},
		{"EET-2EEST,M3.5.0/3,M10.5.0/4", "EET", 2 * 3600, true},
		{"EST5EDT", "EST", -5 * 3600, true},
		{"<+03>-3", "+03", 3 * 3600, true},
		{"IST-5:30", "IST", 5*3600 + 1800, true},
		{"UTC0", "UTC", 0, true},
		{"Europe/Istanbul", "", 0, false},
		{"", "", 0, false},
	} {
		name, off, ok := parsePosixTZ(tc.in)
		if name != tc.name || off != tc.off || ok != tc.ok {
			t.Errorf("parsePosixTZ(%q) = %q,%d,%v want %q,%d,%v", tc.in, name, off, ok, tc.name, tc.off, tc.ok)
		}
	}
}
