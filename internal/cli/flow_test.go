package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// router sends SAP binaries to the fake and cp/ls/chown to the real runner.
type router struct {
	fake *exec.Fake
	real exec.Runner
}

var fakeBins = map[string]bool{"sapcontrol": true, "SAPCAR": true, "disp+work": true, "saphostctrl": true, "saphostexec": true}

func (r router) Run(ctx context.Context, c exec.Cmd) (exec.Result, error) {
	if fakeBins[filepath.Base(c.Path)] {
		return r.fake.Run(ctx, c)
	}
	return r.real.Run(ctx, c)
}

func (r router) LookPath(f string) (string, error) {
	if fakeBins[f] {
		return r.fake.LookPath(f)
	}
	return r.real.LookPath(f)
}

// flowEnv is a scripted SAP host: kernel dir with stub binaries, download
// dir with today's archives, fake sapcontrol answering for ABC (D00, ASCS01).
type flowEnv struct {
	fake      *exec.Fake
	target    *system.Target
	kernelDir string
	download  string
	sc        string
}

func okBody(fn string) string { return "\n27.09.2026 10:00:00\n" + fn + "\nOK\n" }

func procs(status string, exit int) exec.Result {
	return exec.Result{Stdout: "\n27.09.2026 10:00:00\nGetProcessList\nOK\nname, description, dispstatus, textstatus, starttime, elapsedtime, pid\n" +
		"disp+work, Dispatcher, " + status + ", Running, 2026 09 27 08:00:00, 1:00:00, 1\n", ExitCode: exit}
}

var down = exec.Result{Stdout: "\n27.09.2026 10:00:00\nGetProcessList\nFAIL: NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()\n", ExitCode: 1}

func dispworkV(patch int) exec.Result {
	return exec.Result{Stdout: fmt.Sprintf("kernel release                793\n\ncompiled on                   Linux GNU SLES-12 x86_64 cc9.3.1 for linuxx86_64\n\ncompilation mode              UNICODE\n\npatch number                  %d\n", patch)}
}

func newFlow(t *testing.T) *flowEnv {
	t.Helper()
	root := t.TempDir()
	kdir := filepath.Join(root, "sapmnt", "ABC", "exe", "uc", "linuxx86_64")
	dl := filepath.Join(root, "download")
	for _, d := range []string{kdir, dl} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"disp+work", "gwrd", "icman", "msg_server", "sapstartsrv", "SAPCAR", "saproot.sh", "sapcpe", "libsapu16.so"} {
		if err := os.WriteFile(filepath.Join(kdir, f), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	old := now.Add(-96 * time.Hour)
	for name, mt := range map[string]time.Time{
		"SAPEXE_403-80007807.SAR": now, "SAPEXEDB_403-80007808.SAR": now, "dw_421-80007541.sar": now, "dw_423-80007541.sar": now,
		"SAPEXE_390-80007000.SAR": old, "SAPEXEDB_390-80007001.SAR": old,
	} {
		p := filepath.Join(dl, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("k"), 4096), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	f := exec.NewFake()
	f.Paths["sapcontrol"] = "/usr/sap/hostctrl/exe/sapcontrol"
	sc := "/usr/sap/hostctrl/exe/sapcontrol -nr "
	f.On(sc+"00 -function StopSystem ALL", okBody("StopSystem"), 0).
		On(sc+"00 -function WaitforStopped 600 10", okBody("WaitforStopped"), 0).On(sc+"01 -function WaitforStopped 600 10", okBody("WaitforStopped"), 0).
		On(sc+"00 -function StopService", okBody("StopService"), 0).On(sc+"01 -function StopService", okBody("StopService"), 0).
		On(sc+"00 -function StartService ABC", okBody("StartService"), 0).On(sc+"01 -function StartService ABC", okBody("StartService"), 0).
		On(sc+"00 -function StartSystem ALL", okBody("StartSystem"), 0).
		On(sc+"00 -function WaitforStarted 900 10", okBody("WaitforStarted"), 0).On(sc+"01 -function WaitforStarted 900 10", okBody("WaitforStarted"), 0)
	sapcar := filepath.Join(kdir, "SAPCAR")
	for _, n := range []string{"SAPEXE_403-80007807.SAR", "SAPEXEDB_403-80007808.SAR", "dw_421-80007541.sar", "dw_423-80007541.sar"} {
		out := "x disp+work\nx gwrd\nx icman\nx msg_server\nx sapstartsrv\nx libsapu16.so\n"
		if strings.HasPrefix(n, "dw_") {
			out = "x disp+work\n"
		}
		f.On(sapcar+" -xvf "+filepath.Join(kdir, n), out, 0)
	}
	f.On(filepath.Join(kdir, "disp+work")+" -V", dispworkV(200).Stdout, 0)

	tgt := &system.Target{SID: "ABC", SIDAdm: "abcadm", Group: "sapsys", KernelDir: kdir, DirExeRoot: "/usr/sap/ABC/SYS/exe",
		StateDir: filepath.Join(root, "state"), Sapcontrol: "/usr/sap/hostctrl/exe/sapcontrol", Source: "sapcontrol ParameterValue DIR_CT_RUN (instance 00)",
		Instances: []discovery.Instance{{SID: "ABC", Nr: "00", Name: "D00", Type: "D", Host: "sapci"}, {SID: "ABC", Nr: "01", Name: "ASCS01", Type: "ASCS", Host: "sapci"}}}
	os.MkdirAll(tgt.StateDir, 0o755)
	return &flowEnv{fake: f, target: tgt, kernelDir: kdir, download: dl, sc: sc}
}

// install wires the flow environment into the CLI for one test.
func (fe *flowEnv) install(t *testing.T) {
	t.Helper()
	prevRunner, prevResolve, prevCollect, prevRoot := runner, resolveTarget, collectStatus, isRoot
	runner = router{fake: fe.fake, real: exec.NewReal()}
	isRoot = false
	resolveTarget = func(context.Context, string) (*system.Target, []string, error) {
		fe.target.Snapshot, _ = system.LoadSnapshot(fe.target.StateDir)
		return fe.target, nil, nil
	}
	collectStatus = func(context.Context, status.Options) *status.Report { return exampleReport() }
	t.Cleanup(func() { runner, resolveTarget, collectStatus, isRoot = prevRunner, prevResolve, prevCollect, prevRoot })
}

// runMenu drives one menu session and optionally records the transcript.
func runMenu(t *testing.T, name, input string) string {
	t.Helper()
	pal := ui.Palette{Colour: true, Unicode: true}
	var out bytes.Buffer
	Menu(strings.NewReader(input), &out, pal)
	if os.Getenv("KERNELMAN_WRITE_EXAMPLE") == "1" && name != "" {
		content := "$ ./kernelman.sh              # menü · " + strings.ReplaceAll(strings.TrimSpace(input), "\n", " ⏎ ") + "\n" + out.String()
		if err := os.WriteFile("../../docs/examples/"+name+".txt", []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ui.Strip(out.String())
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q\n%s", w, out)
		}
	}
}

func TestFlowMenuStatus(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	out := runMenu(t, "menu", "1\n\nq\n")
	mustContain(t, out, "1) ✔ SAP Status", "SYSTEM ABC", "Press Enter to return to the main menu")
}

func TestFlowBackup(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	out := runMenu(t, "backup", "2\n\n\nq\n") // 2, confirm (Enter = yes), Enter back, quit
	want := filepath.Join(filepath.Dir(fe.kernelDir), "exe_"+time.Now().Format("20060102"))
	mustContain(t, out, "[2/4] Copy to "+want, "[3/4] Verify copy ... ok  9 files match", "Backup directory "+want, "disp+work", "Backup ready", "2) ✔ Kernel Backup")
	if _, err := os.Stat(filepath.Join(want, "gwrd")); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

func TestFlowFiles(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	out := runMenu(t, "files", "3\n"+fe.download+"\n\n\nq\n") // 3, directory, confirm, Enter back, quit
	mustContain(t, out, "Archives dated today, in apply order", "1  SAPEXE_403-80007807.SAR", "4  dw_423-80007541.sar", "target level after apply: patch 423",
		"2 older archive(s) ignored", "[1/3] Copy 4 archive(s)", "chown skipped", "3) ✔ Kernel Files")
	for _, n := range []string{"SAPEXE_403-80007807.SAR", "dw_423-80007541.sar"} {
		if _, err := os.Stat(filepath.Join(fe.kernelDir, n)); err != nil {
			t.Errorf("%s not copied", n)
		}
	}
	if _, err := os.Stat(filepath.Join(fe.kernelDir, "SAPEXE_390-80007000.SAR")); err == nil {
		t.Error("old archive must not be copied")
	}
}

func TestFlowStop(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	// lights before, Stop's own probe, lights after → GREEN, GREEN, then down
	fe.fake.OnSeq(fe.sc+"00 -function GetProcessList", procs("GREEN", 3), procs("GREEN", 3), down)
	fe.fake.OnSeq(fe.sc+"01 -function GetProcessList", procs("GREEN", 3), procs("GREEN", 3), down)
	out := runMenu(t, "stop", "4\nK\n\nq\n") // 4, K = stop, Enter back, quit
	mustContain(t, out, "S = Start · K = Stop (Kapat)", "[1/5] StopSystem ALL ... ok", "[5/5] StopService ASCS01 (01) ... ok", "system ABC stopped",
		"D00 (sapstartsrv down)", "4) ✔ SAP Stop / Start")
}

func TestFlowUpdateAndStart(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	// prepare: today's backup and copied archives, as Kernel Backup / Kernel Files would have done
	ctx := context.Background()
	e := &ops.Env{R: router{fake: fe.fake, real: exec.NewReal()}, P: platformNow(), T: fe.target, Pr: progress{w: &bytes.Buffer{}, pal: ui.Palette{}}}
	if _, err := ops.Backup(ctx, e); err != nil {
		t.Fatal(err)
	}
	today, _, _ := ops.ScanSARs(fe.download, time.Now())
	if _, err := ops.CopySARs(ctx, e, today); err != nil {
		t.Fatal(err)
	}
	// system is down while updating; after StartService it answers again
	fe.fake.OnSeq(fe.sc+"00 -function GetProcessList", down, procs("GREEN", 3))
	fe.fake.OnSeq(fe.sc+"01 -function GetProcessList", down, procs("GREEN", 3))
	fe.fake.OnSeq(filepath.Join(fe.kernelDir, "disp+work")+" -V", dispworkV(200), dispworkV(423))

	out := runMenu(t, "update", "5\n\nS\n\nq\n") // 5, confirm order (Enter), S = start afterwards, Enter back, quit
	mustContain(t, out, "backup from today", "1  SAPEXE_403-80007807.SAR", "[1/7] SAPCAR -xvf SAPEXE_403-80007807.SAR  (SAPEXE 403) ... ok  6 files",
		"[4/7] SAPCAR -xvf dw_423-80007541.sar  (DW 423) ... ok  1 files", "[7/7] Read kernel version (disp+work -V) ... ok  793 patch 423",
		"Kernel ABC: 793 patch 200 → 793 patch 423", "[3/5] StartSystem ALL ... ok", "system ABC started", "5) ✔ Kernel Update")
}

func TestFlowRollback(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	e := &ops.Env{R: router{fake: fe.fake, real: exec.NewReal()}, P: platformNow(), T: fe.target, Pr: progress{w: &bytes.Buffer{}, pal: ui.Palette{}}}
	bk, err := ops.Backup(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(fe.kernelDir, "gwrd"), []byte("broken"), 0o755)
	fe.fake.OnSeq(fe.sc+"00 -function GetProcessList", down)
	fe.fake.OnSeq(fe.sc+"01 -function GetProcessList", down)
	fe.fake.OnSeq(filepath.Join(fe.kernelDir, "disp+work")+" -V", dispworkV(423), dispworkV(200))
	out := runMenu(t, "rollback", "6\n\ny\n\n\nq\n") // 6, backup default, confirm y, no start, Enter back, quit
	mustContain(t, out, "Backup to restore ["+bk.Dest+"]", "[1/4] Copy "+bk.Dest, "Kernel ABC restored: 793 patch 423 → 793 patch 200", "6) ✔ Kernel Rollback")
	b, _ := os.ReadFile(filepath.Join(fe.kernelDir, "gwrd"))
	if string(b) == "broken" {
		t.Error("gwrd not restored")
	}
}
