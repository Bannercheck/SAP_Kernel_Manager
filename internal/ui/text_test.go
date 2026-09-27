package ui

import (
	"bytes"
	"testing"
)

func TestCleanInput(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		esc  bool
	}{
		{"n\n", "n", false},
		{"\x1bn\n", "n", true},         // Esc then N: the sequence that garbled terminals
		{"\x1b\n", "", true},           // Esc alone
		{"\x1b[A\x1b[Dy\n", "y", true}, // arrow keys then Y
		{"\x1bOPm\n", "m", true},       // F1 (SS3) then M
		{"a\tb\x7f\r\n", "a b", false}, // tab, DEL, CR
		{"/home/tcxxx/kernel\n", "/home/tcxxx/kernel", false},
	} {
		got, esc := CleanInput(tc.in)
		if got != tc.want || esc != tc.esc {
			t.Errorf("CleanInput(%q) = %q,%v want %q,%v", tc.in, got, esc, tc.want, tc.esc)
		}
	}
}

func TestASCIIWriter(t *testing.T) {
	var out bytes.Buffer
	w := ASCIIWriter(&out)
	in := []byte("Hostname host1 · exe → exe_20260927 … ✔ ├── /usr/sap › ağaç\n")
	for i := range in { // one byte at a time: multi-byte runes are split across writes
		if _, err := w.Write(in[i : i+1]); err != nil {
			t.Fatal(err)
		}
	}
	want := "Hostname host1 - exe -> exe_20260927 ... + |-- /usr/sap > ağaç\n"
	if out.String() != want {
		t.Errorf("got %q\nwant %q", out.String(), want)
	}
}
