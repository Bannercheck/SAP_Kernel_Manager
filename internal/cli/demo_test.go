package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// TestDemoFlow runs the whole menu against the simulated host: status,
// backup, files, stop, update (+start), rollback.
func TestDemoFlow(t *testing.T) {
	prevRunner, prevPlat, prevUsr, prevRoot, prevCollect, prevResolve := runner, plat, system.UsrSap, demoRoot, collectStatus, resolveTarget
	t.Cleanup(func() {
		runner, plat, system.UsrSap, demoRoot, collectStatus, resolveTarget = prevRunner, prevPlat, prevUsr, prevRoot, prevCollect, prevResolve
	})
	if err := EnableDemo(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	prevCT := ops.ChangeTime
	ops.ChangeTime = func(os.FileInfo) time.Time { return time.Time{} }
	t.Cleanup(func() { ops.ChangeTime = prevCT })
	scanRoots = []string{demoRoot} // not the home directory of the build machine
	input := strings.Join([]string{
		"1", "m", // SAP Status, main menu
		"2", "y", "m", // Kernel Backup: yes, main menu
		"4", "y", "m", // Kernel File Transfer: yes (server scan, no directory question), main menu
		"5", "k", "m", // SAP Stop / Start: K = stop, main menu
		"6", "y", "y", "m", // Kernel Update: confirm order, Y = start afterwards, main menu
		"7", "", "y", "y", "n", "m", // Rollback: default backup (Enter keeps it), Y = stop the running system, Y = confirm, N = no start, main menu
		"q",
	}, "\n") + "\n"
	out := runMenu(t, "", input)
	for _, want := range []string{
		"DEMO · simulated SAP host",
		"SYSTEM ABC · AS ABAP",
		"1) ✔ SAP Status", "2) ✔ Kernel Backup", "4) ✔ Kernel File Transfer", "5) ✔ SAP Stop / Start", "6) ✔ Kernel Update", "7) ✔ Kernel Rollback",
		"[1/11] Check free space", "[4/11] Verify linuxx86_64_", "target level after apply: patch 423",
		"[1/5] StopSystem ALL ... ok", "D00 (sapstartsrv down)",
		"Kernel ABC: 793 Patch 200 → 793 Patch 423", "system ABC started",
		"system ABC is running; the kernel can only be replaced while it is stopped.",
		"Kernel ABC restored: 793 Patch 423 → 793 Patch 200",
		"Backup ready: 3 directories",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("demo transcript lacks %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "✘") {
		t.Errorf("demo flow reported a failure:\n%s", out)
	}
}
