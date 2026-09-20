package cli

import (
	"bytes"
	"context"
	"os"
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
		"SYSTEM  TYPE  SAP SYSTEM   KERNEL                              SAPSTARTSRV      INSTANCES",
		"ABC     ABAP  (~) partial  793 patch 200 (changelist 2123456)  (+) 2/2 running  D00 (+)  ASCS01 (+)  sapapp2/02 (x)",
		"QAS     ABAP  (x) stopped  793 patch 150                       (x) 0/2 running  ASCS10 (x)  D11 (x)",
		"SAP Host Agent  (+) running  722 patch 65",
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

// TestMenuExample drives the menu against the example report and, when
// KERNELMAN_WRITE_EXAMPLE=1, records docs/examples/menu.txt for the screenshot.
func TestMenuExample(t *testing.T) {
	collectStatus = func(context.Context, status.Options) *status.Report { return exampleReport() }
	defer func() { collectStatus = liveCollect }()

	pal := ui.Palette{Colour: true, Unicode: true}
	var out bytes.Buffer
	Menu(strings.NewReader("1\n\nq\n"), &out, pal)
	for _, want := range []string{"1) " + pal.Check() + " SAP Status", "SYSTEM ABC", "SYSTEM QAS", "DIR_CT_RUN"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("menu transcript lacks %q:\n%s", want, out.String())
		}
	}
	if os.Getenv("KERNELMAN_WRITE_EXAMPLE") == "1" {
		content := "$ ./kernelman.sh              # argümansız: menü · 1 = SAP Status, q = çıkış\n" + out.String()
		if err := os.WriteFile("../../docs/examples/menu.txt", []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
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
