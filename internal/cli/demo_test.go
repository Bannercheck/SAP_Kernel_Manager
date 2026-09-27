package cli

import (
	"strings"
	"testing"

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
	input := strings.Join([]string{
		"1", "0", // SAP Status, back
		"2", "1", "0", // Kernel Backup: yes, back
		"3", "1", "0", // Kernel Files: yes (server scan, no directory question), back
		"4", "2", "0", // SAP Stop / Start: 2 = stop, back
		"5", "1", "1", "0", // Kernel Update: confirm order, 1 = start afterwards, back
		"6", "", "1", "1", "0", "0", // Rollback: default backup (Enter keeps it), 1 = stop the running system, 1 = confirm, 0 = no start, back
		"q",
	}, "\n") + "\n"
	out := runMenu(t, "", input)
	for _, want := range []string{
		"DEMO · simulated SAP host",
		"SYSTEM ABC · AS ABAP",
		"1) ✔ SAP Status", "2) ✔ Kernel Backup", "3) ✔ Kernel Files", "4) ✔ SAP Stop / Start", "5) ✔ Kernel Update", "6) ✔ Kernel Rollback",
		"[3/10] Verify linuxx86_64_", "target level after apply: patch 423",
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
