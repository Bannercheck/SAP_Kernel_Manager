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
		"SYSTEM          TYPE     HOSTNAME  KERNEL                INSTANCES",
		"(~) PARTIAL  ABC             AS ABAP  sapci     Kernel 793 Patch 200  D00 (+)  ASCS01 (+)  sapapp2/02 (x)",
		"(x) STOPPED  QAS             AS ABAP  sapci     Kernel 793 Patch 150  ASCS10 (x)  D11 (x)",
		"(+) RUNNING  SAP Host Agent           sapci     Kernel 722 Patch 65",
		"DISK",
		"/usr/sap  43.3 GB   (+) 117.9 GB free of 199.9 GB (41% used, /usr/sap)",
		"|-- ABC            32.0 GB", "|-- QAS            10.0 GB", "`-- tmp            2.0 MB",
		"/sapmnt  21.0 GB   (~) 8.0 GB free of 50.0 GB (84% used, /sapmnt)", "`-- QAS            9.0 GB",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("summary lacks %q\n%s", want, joined)
		}
	}
	empty := SummaryLines(&status.Report{}, pal)
	if len(empty) != 3 || !strings.Contains(empty[1], "no SAP instances") || !strings.Contains(empty[2], "MISSING") {
		t.Errorf("empty summary = %q", empty)
	}
}

func TestMenuMarksResults(t *testing.T) {
	summaryFunc = func(ui.Palette) []string { return []string{"(+) ABC  ABAP  running"} }
	collectStatus = func(context.Context, status.Options) *status.Report { return exampleReport() }
	defer func() { summaryFunc = liveSummary }()

	in := strings.NewReader("1\n\nm\nzzz\nq\n") // SAP Status ok, Enter is ignored, M = main menu, bad choice, quit
	var out bytes.Buffer
	if code := Menu(in, &out, ui.Palette{}); code != ExitOK {
		t.Fatalf("exit code %d", code)
	}
	s := out.String()
	for _, want := range []string{
		"(+) ABC  ABAP  running",
		"1) OK SAP Status",
		"--- SAP Status: OK done",
		"[M] Main menu  [Q] Quit",
		`!! unknown choice "zzz"`,
		"7)   Kernel Rollback", "8)   Send to Other Servers",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("menu output lacks %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "9)") || strings.Contains(s, ") SAP Stop\n") {
		t.Errorf("command-line-only operations must not be listed:\n%s", s)
	}
}

func TestMenuChoice(t *testing.T) {
	for _, in := range []string{"1", "status", "SAP Status", "sap status"} {
		if op, ok := menuChoice(in); !ok || op.ID != "status" {
			t.Errorf("menuChoice(%q) = %v %v", in, op.ID, ok)
		}
	}
	if op, ok := menuChoice("5"); !ok || op.ID != "control" {
		t.Errorf("menuChoice(5) = %v %v", op.ID, ok)
	}
	if op, ok := menuChoice("3"); !ok || op.ID != "download" {
		t.Errorf("menuChoice(3) = %v %v", op.ID, ok)
	}
	if _, ok := menuChoice("99"); ok {
		t.Error("99 should be rejected")
	}
}
