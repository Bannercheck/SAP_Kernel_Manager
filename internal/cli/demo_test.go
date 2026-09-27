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
		"1", "", // SAP Status
		"2", "", "", // Kernel Backup: confirm, back
		"3", "", "", "", // Kernel Files: default download dir, confirm, back
		"4", "K", "", // stop
		"5", "", "S", "", // Kernel Update: confirm order, start afterwards, back
		"6", "", "K", "y", "", "", // Rollback: default backup, K = stop the running system, confirm, no start, back
		"q",
	}, "\n") + "\n"
	out := runMenu(t, "", input)
	for _, want := range []string{
		"DEMO · simulated SAP host",
		"SYSTEM ABC · ABAP",
		"1) ✔ SAP Status", "2) ✔ Kernel Backup", "3) ✔ Kernel Files", "4) ✔ SAP Stop / Start", "5) ✔ Kernel Update", "6) ✔ Kernel Rollback",
		"[3/4] Verify copy ... ok", "target level after apply: patch 423",
		"[1/5] StopSystem ALL ... ok", "D00 (sapstartsrv down)",
		"Kernel ABC: 793 patch 200 → 793 patch 423", "system ABC started",
		"system ABC is running; the kernel can only be replaced while it is stopped.",
		"Kernel ABC restored: 793 patch 423 → 793 patch 200",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("demo transcript lacks %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "✘") {
		t.Errorf("demo flow reported a failure:\n%s", out)
	}
}
