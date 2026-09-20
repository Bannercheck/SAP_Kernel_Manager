package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

func TestSummaryLines(t *testing.T) {
	pal := ui.Palette{} // ASCII, no colour
	lines := SummaryLines(exampleReport(), pal)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "(~) ABC  ABAP") || !strings.Contains(lines[0], "sapstartsrv 2/2") {
		t.Errorf("summary = %q", lines)
	}
}

func TestMenuMarksResults(t *testing.T) {
	summaryFunc = func(ui.Palette) []string { return []string{"(+) ABC  ABAP  running"} }
	defer func() { summaryFunc = liveSummary }()

	in := strings.NewReader("10\n\nstop\n\nzzz\nq\n") // Version ok, SAP Stop planned (fails), bad choice, quit
	var out bytes.Buffer
	if code := Menu(in, &out, ui.Palette{}); code != ExitOK {
		t.Fatalf("exit code %d", code)
	}
	s := out.String()
	for _, want := range []string{
		"(+) ABC  ABAP  running",
		"10) OK Version",
		"2) !! SAP Stop",
		"--- Version: OK done.",
		"--- SAP Stop: !! failed (exit code 1).",
		`!! unknown choice "zzz"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("menu output lacks %q\n%s", want, s)
		}
	}
}

func TestMenuChoice(t *testing.T) {
	for _, in := range []string{"1", "status", "SAP Status", "sap status"} {
		if op, ok := menuChoice(in); !ok || op.ID != "status" {
			t.Errorf("menuChoice(%q) = %v %v", in, op.ID, ok)
		}
	}
	if _, ok := menuChoice("99"); ok {
		t.Error("99 should be rejected")
	}
}
