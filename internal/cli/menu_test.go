package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

func TestSummaryLines(t *testing.T) {
	pal := ui.Palette{} // ASCII, no colour
	lines := SummaryLines(exampleReport(), pal)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"(~)  ABC             ABAP  kernel 793 patch 200 (changelist 2123456)  D00 (+)  ASCS01 (+)  sapapp2/02 (x)",
		"(x)  QAS             ABAP  kernel 793 patch 150                       ASCS10 (x)  D11 (x)",
		"(+)  SAP Host Agent        722 patch 65",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("summary lacks %q\n%s", want, joined)
		}
	}
	empty := SummaryLines(&status.Report{}, pal)
	if len(empty) != 2 || !strings.Contains(empty[0], "no SAP instances") || !strings.Contains(empty[1], "not installed") {
		t.Errorf("empty summary = %q", empty)
	}
}

func TestMenuMarksResults(t *testing.T) {
	summaryFunc = func(ui.Palette) []string { return []string{"(+) ABC  ABAP  running"} }
	collectStatus = func(context.Context, status.Options) *status.Report { return exampleReport() }
	defer func() { summaryFunc = liveSummary }()

	in := strings.NewReader("1\n\nzzz\nq\n") // SAP Status ok, bad choice, quit
	var out bytes.Buffer
	if code := Menu(in, &out, ui.Palette{}); code != ExitOK {
		t.Fatalf("exit code %d", code)
	}
	s := out.String()
	for _, want := range []string{
		"(+) ABC  ABAP  running",
		"1) OK SAP Status",
		"--- SAP Status: OK done. Press Enter to return to the main menu.",
		`!! unknown choice "zzz"`,
		"6)   Kernel Rollback",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("menu output lacks %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "7)") || strings.Contains(s, "Version") {
		t.Errorf("command-line-only operations must not be listed:\n%s", s)
	}
}

func TestMenuChoice(t *testing.T) {
	for _, in := range []string{"1", "status", "SAP Status", "sap status"} {
		if op, ok := menuChoice(in); !ok || op.ID != "status" {
			t.Errorf("menuChoice(%q) = %v %v", in, op.ID, ok)
		}
	}
	if op, ok := menuChoice("4"); !ok || op.ID != "control" {
		t.Errorf("menuChoice(4) = %v %v", op.ID, ok)
	}
	if _, ok := menuChoice("99"); ok {
		t.Error("99 should be rejected")
	}
}
