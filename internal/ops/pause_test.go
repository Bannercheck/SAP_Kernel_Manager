package ops

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

func paramBody(v string) string { return "\n27.09.2026 10:00:00\nParameterValue\nOK\n" + v + "\n" }

// A running sapstartsrv is stopped only while the kernel files change and is
// started again with its profile; one whose profile is unknown is left alone.
func TestExtractPausesSapstartsrv(t *testing.T) {
	e, fake, rec := newEnv(t)
	sc := "/hc/sapcontrol -nr "
	gray := exec.Result{Stdout: procsGray, ExitCode: 4}
	refused := exec.Result{Stdout: connRefused, ExitCode: 1}
	// 00: probe (answers) → after StopService (refused) → after StartService (answers)
	fake.OnSeq(sc+"00 -function GetProcessList", gray, refused, gray)
	fake.On(sc+"00 -function ParameterValue SAPPROFILE", paramBody("/usr/sap/ABC/SYS/profile/ABC_D00_host"), 0).
		On(sc+"00 -function StopService", okBody("StopService"), 0).On(sc+"00 -function StartService ABC", okBody("StartService"), 0)
	// 01: answers, but its profile cannot be found → left running
	fake.On(sc+"01 -function GetProcessList", procsGray, 4).On(sc+"01 -function ParameterValue SAPPROFILE", "\n27.09.2026 10:00:00\nParameterValue\nFAIL: not found\n", 1)
	sapcar := filepath.Join(e.T.KernelDir, "SAPCAR")
	fake.On(sapcar+" -xvf /dl/SAPEXE_403-1.SAR", "x disp+work\n", 0)
	files := []SARFile{{Name: "SAPEXE_403-1.SAR", Path: "/dl/SAPEXE_403-1.SAR", Component: "SAPEXE", Patch: 403, Full: true}}
	if _, err := Extract(context.Background(), e, sapcar, files); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	out := rec.String()
	for _, want := range []string{"sapstartsrv ASCS01 keeps running: its profile is unknown",
		"[1/11] Stop sapstartsrv D00 (00) while its files are replaced\n  ok started again with pf=ABC_D00_host after the files",
		"[2/11] SAPCAR -xvf SAPEXE_403-1.SAR", "[9/11] Start sapstartsrv D00 (00)\n  ok sapcontrol StartService · answers"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	var order []string
	for _, c := range fake.Calls {
		switch {
		case strings.HasSuffix(c.String(), "StopService"), strings.Contains(c.String(), "StartService"), filepath.Base(c.Path) == "SAPCAR" && c.Args[0] == "-xvf":
			order = append(order, filepath.Base(c.Path)+" "+c.Args[len(c.Args)-1])
		}
	}
	if got := strings.Join(order, ","); got != "sapcontrol StopService,SAPCAR /dl/SAPEXE_403-1.SAR,SAPCAR /dl/SAPEXE_403-1.SAR,SAPCAR /dl/SAPEXE_403-1.SAR,sapcontrol ABC" {
		t.Errorf("order = %s", got)
	}
}

// When extraction fails, the stopped sapstartsrv stays down and the operator is told how it comes back.
func TestExtractFailureKeepsServiceNote(t *testing.T) {
	e, fake, rec := newEnv(t)
	sc := "/hc/sapcontrol -nr "
	fake.OnSeq(sc+"00 -function GetProcessList", exec.Result{Stdout: procsGray, ExitCode: 4}, exec.Result{Stdout: connRefused, ExitCode: 1})
	fake.On(sc+"00 -function ParameterValue SAPPROFILE", paramBody("/p/ABC_D00_host"), 0).On(sc+"00 -function StopService", okBody("StopService"), 0)
	sapcar := filepath.Join(e.T.KernelDir, "SAPCAR")
	fake.On(sapcar+" -xvf /dl/SAPEXE_403-1.SAR", "SAPCAR: could not open for writing disp+work", 1)
	files := []SARFile{{Name: "SAPEXE_403-1.SAR", Path: "/dl/SAPEXE_403-1.SAR", Component: "SAPEXE", Patch: 403, Full: true}}
	if _, err := Extract(context.Background(), e, sapcar, files); err == nil {
		t.Fatal("extraction error not reported")
	}
	if !strings.Contains(rec.String(), "sapstartsrv of D00 stays stopped while the kernel is inconsistent") {
		t.Errorf("no note:\n%s", rec)
	}
}
