package ops

import (
	"strings"
	"testing"
)

func TestCheckKernelSet(t *testing.T) {
	f := func(names ...string) []SARFile {
		var out []SARFile
		for _, n := range names {
			s, _ := ParseSARName(n)
			out = append(out, s)
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		files []SARFile
		want  string // "" = accepted
	}{
		{"pair", f("SAPEXE_403-1.SAR", "SAPEXEDB_403-2.SAR", "dw_421-3.sar"), ""},
		{"single component", f("dw_421-3.sar"), ""},
		{"older SAPEXE superseded", f("SAPEXE_390-1.SAR", "SAPEXE_403-1.SAR", "SAPEXEDB_403-2.SAR"), ""},
		{"db part alone", f("SAPEXEDB_403-2.SAR"), "SAPEXEDB 403 without SAPEXE"},
		{"exe part alone", f("SAPEXE_403-1.SAR", "dw_421-3.sar"), "SAPEXE 403 without SAPEXEDB"},
		{"levels differ", f("SAPEXE_403-1.SAR", "SAPEXEDB_400-2.SAR"), "SAPEXE 403 and SAPEXEDB 400 differ"},
	} {
		err := CheckKernelSet(tc.files)
		if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}
