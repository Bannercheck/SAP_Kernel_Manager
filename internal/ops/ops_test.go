package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// router sends SAP binaries to the fake and everything else (cp, ls, chown) to the real runner.
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

type fakePlatform struct{}

func (fakePlatform) Name() string                                  { return "linux" }
func (fakePlatform) Arch() string                                  { return "amd64" }
func (fakePlatform) KernelDirName() string                         { return "linuxx86_64" }
func (fakePlatform) ExeSuffix() string                             { return "" }
func (fakePlatform) LibPathVar() string                            { return "LD_LIBRARY_PATH" }
func (fakePlatform) SIDAdmUser(sid string) string                  { return strings.ToLower(sid) + "adm" }
func (fakePlatform) IsPrivileged() bool                            { return false }
func (fakePlatform) HostctrlExeDir() string                        { return "/nonexistent" }
func (fakePlatform) SapservicesPath() string                       { return "" }
func (fakePlatform) OSVersion(context.Context, exec.Runner) string { return "test" }

type recorder struct{ lines []string }

func (r *recorder) Begin(i, n int, title string) {
	r.lines = append(r.lines, fmt.Sprintf("[%d/%d] %s", i, n, title))
}
func (r *recorder) End(err error, detail string, _ time.Duration) {
	if err != nil {
		r.lines = append(r.lines, "  FAIL "+err.Error())
		return
	}
	r.lines = append(r.lines, "  ok "+detail)
}
func (r *recorder) Info(line string)         { r.lines = append(r.lines, "  info "+line) }
func (r *recorder) Block(title, text string) { r.lines = append(r.lines, "  block "+title) }
func (r *recorder) String() string           { return strings.Join(r.lines, "\n") }

func okBody(fn string) string { return "\n27.09.2026 10:00:00\n" + fn + "\nOK\n" }

const procsGreen = "\n27.09.2026 10:00:00\nGetProcessList\nOK\nname, description, dispstatus, textstatus, starttime, elapsedtime, pid\ndisp+work, Dispatcher, GREEN, Running, 2026 09 27 08:00:00, 1:00:00, 1\n"
const procsGray = "\n27.09.2026 10:00:00\nGetProcessList\nOK\nname, description, dispstatus, textstatus, starttime, elapsedtime, pid\ndisp+work, Dispatcher, GRAY, Stopped, , , 0\n"
const connRefused = "\n27.09.2026 10:00:00\nGetProcessList\nFAIL: NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()\n"

func dispworkV(patch int) string {
	return fmt.Sprintf("kernel release                793\n\ncompiled on                   Linux GNU SLES-12 x86_64 for linuxx86_64\n\npatch number                  %d\n", patch)
}

// newEnv builds a kernel directory with a few files and a resolved target.
func newEnv(t *testing.T) (*Env, *exec.Fake, *recorder) {
	t.Helper()
	root := t.TempDir()
	kdir := filepath.Join(root, "exe", "uc", "linuxx86_64")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"disp+work", "gwrd", "SAPCAR", "saproot.sh"} {
		if err := os.WriteFile(filepath.Join(kdir, f), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fake := exec.NewFake()
	fake.Paths["sapcontrol"] = "/hc/sapcontrol"
	fake.On(filepath.Join(kdir, "disp+work")+" -V", dispworkV(200), 0)
	tgt := &system.Target{SID: "ABC", SIDAdm: "abcadm", Group: "sapsys", KernelDir: kdir, StateDir: t.TempDir(), Sapcontrol: "/hc/sapcontrol",
		Instances: []discovery.Instance{{SID: "ABC", Nr: "00", Name: "D00"}, {SID: "ABC", Nr: "01", Name: "ASCS01"}}}
	rec := &recorder{}
	fixed := time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)
	e := &Env{R: router{fake: fake, real: exec.NewReal()}, P: fakePlatform{}, T: tgt, Pr: rec, Now: func() time.Time { return fixed }}
	return e, fake, rec
}

func TestBackup(t *testing.T) {
	e, _, rec := newEnv(t)
	res, err := Backup(context.Background(), e)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	want := filepath.Join(filepath.Dir(e.T.KernelDir), "exe_20260927")
	if res.Dest != want || res.Files != 4 || !strings.Contains(res.Listing, "disp+work") {
		t.Errorf("res=%+v\n%s", res, rec)
	}
	if e.T.Snapshot == nil || e.T.Snapshot.LastBackup != want {
		t.Errorf("snapshot not updated: %+v", e.T.Snapshot)
	}
	// second backup on the same day gets a time suffix
	res2, err := Backup(context.Background(), e)
	if err != nil || res2.Dest != want+"_100000" {
		t.Errorf("second backup: %v %+v", err, res2)
	}
	if latest, ok := LatestBackup(e.T.KernelDir); !ok || !strings.HasPrefix(latest, want) {
		t.Errorf("LatestBackup = %q %v", latest, ok)
	}
}

func TestParseAndSort(t *testing.T) {
	cases := map[string][3]any{
		"SAPEXE_403-80007807.SAR":        {"SAPEXE", 403, true},
		"SAPEXE.402_7805.SAR":            {"SAPEXE", 402, true},
		"SAPEXEDB_403-80007808.SAR":      {"SAPEXEDB", 403, true},
		"dw_421-80007541.sar":            {"DW", 421, false},
		"lib_dbsl_403-80007808.sar":      {"LIB_DBSL", 403, false},
		"igsexe_12-80003187.sar":         {"IGSEXE", 12, false},
		"SAPHOSTAGENT65_65-80004822.SAR": {"SAPHOSTAGENT65", 65, false},
	}
	for name, want := range cases {
		f, ok := ParseSARName(name)
		if !ok || f.Component != want[0] || f.Patch != want[1] || f.Full != want[2] {
			t.Errorf("ParseSARName(%s) = %+v %v", name, f, ok)
		}
	}
	if _, ok := ParseSARName("readme.txt"); ok {
		t.Error("readme.txt parsed as archive")
	}
	files := []SARFile{{Name: "dw_423-1.sar", Component: "DW", Patch: 423}, {Name: "SAPEXEDB_402-1.SAR", Component: "SAPEXEDB", Patch: 402, Full: true},
		{Name: "SAPEXE_402-1.SAR", Component: "SAPEXE", Patch: 402, Full: true}, {Name: "igsexe_12-1.sar", Component: "IGSEXE", Patch: 12},
		{Name: "dw_411-1.sar", Component: "DW", Patch: 411}, {Name: "SAPEXE_403-1.SAR", Component: "SAPEXE", Patch: 403, Full: true}}
	SortForApply(files)
	var order []string
	for _, f := range files {
		order = append(order, f.Name)
	}
	want := "igsexe_12-1.sar SAPEXE_402-1.SAR SAPEXEDB_402-1.SAR SAPEXE_403-1.SAR dw_411-1.sar dw_423-1.sar"
	if got := strings.Join(order, " "); got != want {
		t.Errorf("order = %s", got)
	}
	if TargetPatch(files) != 423 {
		t.Errorf("TargetPatch = %d", TargetPatch(files))
	}
}

func TestScanAndCopySARs(t *testing.T) {
	e, _, rec := newEnv(t)
	dl := t.TempDir()
	now := time.Now()
	old := now.Add(-72 * time.Hour)
	for name, mt := range map[string]time.Time{"SAPEXE_403-80007807.SAR": now, "dw_421-80007541.sar": now, "SAPEXE_390-80007000.SAR": old, "notes.txt": now} {
		p := filepath.Join(dl, name)
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	today, older, err := ScanSARs(dl, now)
	if err != nil || len(today) != 2 || len(older) != 1 || today[0].Name != "SAPEXE_403-80007807.SAR" || today[1].Name != "dw_421-80007541.sar" {
		t.Fatalf("scan: %v today=%v older=%v", err, today, older)
	}
	res, err := CopySARs(context.Background(), e, today)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	for _, f := range today {
		if _, err := os.Stat(filepath.Join(e.T.KernelDir, f.Name)); err != nil {
			t.Errorf("%s not copied: %v", f.Name, err)
		}
	}
	if !strings.Contains(res.Listing, "dw_421") || !strings.Contains(res.ChownNote, "skipped") {
		t.Errorf("res=%+v", res)
	}
	if got := e.T.Snapshot.CopiedSARs; len(got) != 2 || e.T.Snapshot.LastDownloadDir != dl {
		t.Errorf("snapshot: %+v", e.T.Snapshot)
	}
}

func TestExtractInOrder(t *testing.T) {
	e, fake, rec := newEnv(t)
	sapcar := filepath.Join(e.T.KernelDir, "SAPCAR")
	files := []SARFile{{Name: "dw_421-1.sar", Path: "/dl/dw_421-1.sar", Component: "DW", Patch: 421},
		{Name: "SAPEXE_403-1.SAR", Path: "/dl/SAPEXE_403-1.SAR", Component: "SAPEXE", Patch: 403, Full: true}}
	fake.On(sapcar+" -xvf /dl/SAPEXE_403-1.SAR", "x disp+work\nx gwrd\nx icman\n", 0)
	fake.On(sapcar+" -xvf /dl/dw_421-1.sar", "x disp+work\n", 0)
	calls := 0
	fake.Responses[filepath.Join(e.T.KernelDir, "disp+work")+" -V"] = exec.Result{Stdout: dispworkV(200)}
	_ = calls
	res, err := Extract(context.Background(), e, sapcar, files)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	if len(res.Steps) != 2 || res.Steps[0].File != "SAPEXE_403-1.SAR" || res.Steps[0].Files != 3 || res.Steps[1].File != "dw_421-1.sar" {
		t.Errorf("steps = %+v", res.Steps)
	}
	if !strings.Contains(res.ChownNote, "skipped") || !strings.Contains(res.SaprootNote, "needs root") {
		t.Errorf("notes: %q %q", res.ChownNote, res.SaprootNote)
	}
	// the extraction order must be SAPEXE_403 before dw_421
	var order []string
	for _, c := range fake.Calls {
		if filepath.Base(c.Path) == "SAPCAR" {
			order = append(order, filepath.Base(c.Args[1]))
			if c.Dir != e.T.KernelDir {
				t.Errorf("SAPCAR must run inside the kernel dir, got %q", c.Dir)
			}
		}
	}
	if strings.Join(order, " ") != "SAPEXE_403-1.SAR dw_421-1.sar" {
		t.Errorf("order = %v", order)
	}
}

func TestRestore(t *testing.T) {
	e, _, rec := newEnv(t)
	bk, err := Backup(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.T.KernelDir, "gwrd"), []byte("broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), e, bk.Dest); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	b, _ := os.ReadFile(filepath.Join(e.T.KernelDir, "gwrd"))
	if string(b) != "#!/bin/sh\nexit 0\n" {
		t.Errorf("gwrd not restored: %q", b)
	}
}

func TestStopAndStart(t *testing.T) {
	e, fake, rec := newEnv(t)
	sc := "/hc/sapcontrol -nr "
	fake.On(sc+"00 -function GetProcessList", procsGreen, 3).On(sc+"01 -function GetProcessList", procsGreen, 3).
		On(sc+"00 -function StopSystem ALL", okBody("StopSystem"), 0).
		On(sc+"00 -function WaitforStopped 600 10", okBody("WaitforStopped"), 0).On(sc+"01 -function WaitforStopped 600 10", okBody("WaitforStopped"), 0).
		On(sc+"00 -function StopService", okBody("StopService"), 0).On(sc+"01 -function StopService", okBody("StopService"), 0)
	if err := Stop(context.Background(), e); err != nil {
		t.Fatalf("stop: %v\n%s", err, rec)
	}
	if !strings.Contains(rec.String(), "[1/5] StopSystem ALL") || !strings.Contains(rec.String(), "[5/5] StopService ASCS01 (01)") {
		t.Errorf("progress:\n%s", rec)
	}

	rec.lines = nil
	fake.On(sc+"00 -function StartService ABC", okBody("StartService"), 0).On(sc+"01 -function StartService ABC", okBody("StartService"), 0).
		On(sc+"00 -function StartSystem ALL", okBody("StartSystem"), 0).
		On(sc+"00 -function WaitforStarted 900 10", okBody("WaitforStarted"), 0).On(sc+"01 -function WaitforStarted 900 10", okBody("WaitforStarted"), 0)
	if err := Start(context.Background(), e); err != nil {
		t.Fatalf("start: %v\n%s", err, rec)
	}
	if !strings.Contains(rec.String(), "[3/5] StartSystem ALL") {
		t.Errorf("progress:\n%s", rec)
	}
	states := Probe(context.Background(), e)
	if IsStopped(states) {
		t.Error("GREEN instances reported as stopped")
	}
	fake.On(sc+"00 -function GetProcessList", connRefused, 1).On(sc+"01 -function GetProcessList", procsGray, 4)
	if !IsStopped(Probe(context.Background(), e)) {
		t.Error("down/GRAY instances reported as running")
	}
}
