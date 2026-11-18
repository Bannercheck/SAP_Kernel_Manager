package hubcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// demo builds the simulated landscape the dashboard demo uses: PRD on four
// hosts, QAS stopped, DEV, one unreachable host; login admin / kernelman1.
func demo(t *testing.T) *hub.Hub {
	t.Helper()
	r := hub.NewDemoRemote()
	r.Delay = 0
	h, err := hub.New(hub.Config{DataDir: t.TempDir(), Remote: r, Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.SetupDemo(h, "admin", "kernelman1"); err != nil {
		t.Fatal(err)
	}
	h.RefreshAll(context.Background())
	return h
}

// session runs the console with typed lines and returns the screen without colours.
func session(t *testing.T, h *hub.Hub, lines ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	c := New(h, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, ui.Palette{Unicode: true})
	code := c.Run()
	if os.Getenv("KERNELMAN_WRITE_EXAMPLE") == "1" && t.Name() != "" {
		name := strings.ToLower(strings.TrimPrefix(t.Name(), "TestConsole"))
		screen := strings.Split(out.String(), "\n")
		if len(screen) > 150 { // a long job console: its start and its end
			screen = append(append(screen[:110:110], fmt.Sprintf("  … %d more lines of the job console …", len(screen)-140)), screen[len(screen)-30:]...)
		}
		content := "$ ./kernelman-hub console      # " + strings.Join(lines[2:], " ⏎ ") + "\n" + strings.Join(screen, "\n")
		_ = os.WriteFile(filepath.Join("..", "..", "docs", "examples", "hub-console-"+name+".txt"), []byte(content), 0o644)
	}
	return ui.Strip(out.String()), code
}

func must(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("screen lacks %q\n%s", w, out)
		}
	}
}

func TestConsoleHome(t *testing.T) {
	h := demo(t)
	out, code := session(t, h, "admin", "kernelman1", "q")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	must(t, out, "KernelMan Hub", "SYSTEM", "PRD", "QAS", "DEV", "793 PL 200", "4 hosts",
		"NEEDS ATTENTION", "sbxsrv", "1) Refresh", "3) Run an operation", "q) Quit")
}

func TestConsoleWrongPassword(t *testing.T) {
	h := demo(t)
	out, code := session(t, h, "admin", "nope", "admin", "nope2", "admin", "nope3")
	if code == 0 || !strings.Contains(out, "wrong user or password") || strings.Contains(out, "3) Run an operation") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

// The whole update from the console: unlock, archives by number, plan,
// SID confirmation, live output with the percentage, 100 % at the end.
func TestConsoleUpdate(t *testing.T) {
	h := demo(t)
	lib := h.Library.List()
	var picks []string
	for i, a := range lib {
		if a.Component == "SAPEXE" || a.Component == "SAPEXEDB" {
			picks = append(picks, strconv.Itoa(i+1))
		}
	}
	out, code := session(t, h, "admin", "kernelman1",
		"3", "PRD", // operation on PRD
		"update", "y", // unlock for 8 hours
		strings.Join(picks, ","), // archives
		"",                       // all hosts
		"y",                      // start afterwards
		"PRD",                    // confirm
		"q")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	must(t, out, "PRD is read-only", "unlocked until", "Target level after the update: patch 423",
		"PLAN", "Back up kernel directories (SAP keeps running)", "Type PRD to confirm",
		"100%", "Kernel Update PRD (4 hosts) → patch 423 finished")
	jobs := h.Jobs.List("PRD", 1)
	if len(jobs) != 1 || jobs[0].State != "ok" || jobs[0].Percent != 100 {
		t.Errorf("job %+v", jobs)
	}
}

func TestConsoleWrongSIDCancels(t *testing.T) {
	h := demo(t)
	h.Store.SetLock("QAS", true)
	out, _ := session(t, h, "admin", "kernelman1", "3", "QAS", "stop", "", "QSA", "q")
	must(t, out, "Type QAS to confirm", "cancelled")
	if len(h.Jobs.List("QAS", 0)) != 0 {
		t.Error("a job ran without the SID")
	}
}

func TestConsoleImportArchive(t *testing.T) {
	h := demo(t)
	f := filepath.Join(t.TempDir(), "dw_430-80007541.sar")
	if err := os.WriteFile(f, []byte("CAR 2.01 dw"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := session(t, h, "admin", "kernelman1", "5", "i", f, "m", "q")
	must(t, out, "dw_430-80007541.sar", "imported")
	if _, err := h.Library.Get("dw_430-80007541.sar"); err != nil {
		t.Error(err)
	}
}
