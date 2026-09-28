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
	d00 := filepath.Join(root, "ABC", "D00", "exe")
	ascs := filepath.Join(root, "ABC", "ASCS01", "exe")
	for _, d := range []string{d00, ascs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"sapstartsrv", "disp+work"} {
			if err := os.WriteFile(filepath.Join(d, f), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	fake := exec.NewFake()
	fake.Paths["sapcontrol"] = "/hc/sapcontrol"
	fake.On(filepath.Join(kdir, "disp+work")+" -V", dispworkV(200), 0)
	tgt := &system.Target{SID: "ABC", SIDAdm: "abcadm", Group: "sapsys", KernelDir: kdir, KernelDirs: []string{kdir, d00, ascs},
		StateDir: t.TempDir(), Sapcontrol: "/hc/sapcontrol",
		Instances: []discovery.Instance{{SID: "ABC", Nr: "00", Name: "D00", ExeDir: d00}, {SID: "ABC", Nr: "01", Name: "ASCS01", ExeDir: ascs}}}
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
	if len(res.Dirs) != 3 {
		t.Fatalf("expected 3 directory backups, got %+v", res.Dirs)
	}
	central := res.Dirs[0]
	want := filepath.Join(filepath.Dir(e.T.KernelDir), "linuxx86_64_20260927")
	if central.Dest != want || central.Files != 4 || !strings.Contains(central.Listing, "disp+work") {
		t.Errorf("central=%+v\n%s", central, rec)
	}
	if d00 := res.Dirs[1]; filepath.Base(d00.Dest) != "exe_20260927" || d00.Files != 2 {
		t.Errorf("instance backup = %+v", d00)
	}
	if res.Files() != 8 {
		t.Errorf("total files = %d", res.Files())
	}
	if _, err := os.Stat(res.LogFile); err != nil {
		t.Errorf("log file: %v", err)
	}
	if e.T.Snapshot == nil || e.T.Snapshot.LastBackup != want || e.T.Snapshot.LastBackups[e.T.KernelDirs[1]] == "" {
		t.Errorf("snapshot not updated: %+v", e.T.Snapshot)
	}
	// second backup on the same day gets a time suffix
	res2, err := Backup(context.Background(), e)
	if err != nil || res2.Dirs[0].Dest != want+"_100000" {
		t.Errorf("second backup: %v %+v", err, res2.Dirs)
	}
	if latest, ok := LatestBackup(e.T.KernelDir); !ok || !strings.HasPrefix(latest, want) {
		t.Errorf("LatestBackup = %q %v", latest, ok)
	}
	if sib := BackupSiblings(e.T.KernelDir); len(sib) != 2 {
		t.Errorf("BackupSiblings = %v", sib)
	}
}

func TestFindTodaySARs(t *testing.T) {
	prevCT := ChangeTime
	ChangeTime = func(os.FileInfo) time.Time { return time.Time{} } // files below are all created now
	defer func() { ChangeTime = prevCT }()
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-72 * time.Hour)
	mk := func(rel string, mt time.Time) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, mt, mt)
	}
	mk("home/basis/downloads/SAPEXE_403-80007807.SAR", now)
	mk("home/basis/downloads/dw_421-80007541.sar", now)
	mk("tmp/kernel/dw_423-80007541.sar", now)
	mk("tmp/kernel/SAPEXE_390-80007000.SAR", old) // older: ignored, only counted
	mk("tmp/kernel/notes.txt", now)
	mk("sapmnt/ABC/exe/uc/linuxx86_64/SAPEXE_403-80007807.SAR", now) // copy inside the kernel dir: excluded ...
	os.MkdirAll(filepath.Join(root, "usr/sap/ABC/SYS/exe"), 0o755)   // ... also when reached as /usr/sap/ABC/SYS/exe/uc
	os.Symlink(filepath.Join(root, "sapmnt/ABC/exe/uc"), filepath.Join(root, "usr/sap/ABC/SYS/exe/uc"))
	for _, f := range []string{"sapstartsrv", "sapcontrol", "disp+work", "dw_423-80007541.sar"} { // today's backup of D00/exe
		mk("usr/sap/ABC/D00/exe_"+now.Format("20060102")+"/"+f, now)
	}
	mk("home/basis/downloads/sapstartsrv", now) // an archive extracted for a look does not make a download dir kernel-like
	mk("home/basis/downloads/sapcontrol", now)
	mk("proc/1/SAPEXE_999-1.SAR", now)                     // pruned
	mk("export/home/tcxxx/SAPEXEDB_403-80007808.SAR", now) // reached through a symlinked /home
	os.Symlink(filepath.Join(root, "export/home"), filepath.Join(root, "home2"))
	exclude := []string{filepath.Join(root, "sapmnt/ABC/exe/uc/linuxx86_64")}
	res, err := FindTodaySARs(context.Background(), ScanOptions{Roots: []string{root}, Day: now, Exclude: exclude})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range res.Today {
		names = append(names, f.Name)
	}
	if strings.Join(names, " ") != "SAPEXE_403-80007807.SAR SAPEXEDB_403-80007808.SAR dw_421-80007541.sar dw_423-80007541.sar" {
		t.Errorf("today = %v", names)
	}
	if res.Older != 1 || len(res.Duplicates) != 0 || res.Dirs == 0 || res.Today[0].Label != "SAPEXE" || res.Today[2].Label != "dw" || res.Unreadable != 0 {
		t.Errorf("res = %+v", res)
	}
	if res.KernelDirs != 1 || res.InKernel != 1 {
		t.Errorf("backup directory not skipped: kernel dirs %d, archives inside %d", res.KernelDirs, res.InKernel)
	}
	for _, f := range res.Today {
		if strings.Contains(f.Path, "exe_") || strings.Contains(f.Path, "/usr/sap/") || strings.Contains(f.Path, "/sapmnt/") {
			t.Errorf("archive from a kernel directory or backup offered: %s", f.Path)
		}
	}
	// A file uploaded today with scp -p / WinSCP keeps its old modification
	// time but gets today's change time: it counts as placed today.
	ChangeTime = func(info os.FileInfo) time.Time {
		if info.Name() == "SAPEXE_390-80007000.SAR" {
			return now
		}
		return time.Time{}
	}
	res, _ = FindTodaySARs(context.Background(), ScanOptions{Roots: []string{root}, Day: now, Exclude: exclude})
	if len(res.Today) != 5 || res.Older != 0 || res.Today[0].Name != "SAPEXE_390-80007000.SAR" || !res.Today[0].Placed.Equal(now) {
		t.Errorf("copied-today archive: %+v", res)
	}
	ChangeTime = func(os.FileInfo) time.Time { return time.Time{} }
	if os.Geteuid() != 0 { // an unreadable directory is counted, not fatal
		locked := filepath.Join(root, "home", "locked")
		os.MkdirAll(locked, 0o000)
		defer os.Chmod(locked, 0o755)
		res, _ = FindTodaySARs(context.Background(), ScanOptions{Roots: []string{root}, Day: now})
		if res.Unreadable != 1 || len(res.UnreadEx) != 1 {
			t.Errorf("unreadable = %d %v", res.Unreadable, res.UnreadEx)
		}
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
	prevCT := ChangeTime
	ChangeTime = func(os.FileInfo) time.Time { return time.Time{} }
	defer func() { ChangeTime = prevCT }()
	today, older, err := ScanSARs(dl, now)
	if err != nil || len(today) != 2 || len(older) != 1 || today[0].Name != "SAPEXE_403-80007807.SAR" || today[1].Name != "dw_421-80007541.sar" {
		t.Fatalf("scan: %v today=%v older=%v", err, today, older)
	}
	res, err := CopySARs(context.Background(), e, today)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	if len(res.Dests) != 3 {
		t.Errorf("copied into %d dirs, want 3", len(res.Dests))
	}
	for _, dir := range e.T.KernelDirs {
		for _, f := range today {
			if _, err := os.Stat(filepath.Join(dir, f.Name)); err != nil {
				t.Errorf("%s not copied to %s: %v", f.Name, dir, err)
			}
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
	if len(res.Steps) != 6 || res.Steps[0].File != "SAPEXE_403-1.SAR" || res.Steps[0].Files != 3 || res.Steps[1].File != "dw_421-1.sar" ||
		res.Steps[2].Dir != e.T.KernelDirs[1] {
		t.Errorf("steps = %+v", res.Steps)
	}
	if !strings.Contains(res.ChownNote, "skipped") || !strings.Contains(res.SaprootNote, "needs root") {
		t.Errorf("notes: %q %q", res.ChownNote, res.SaprootNote)
	}
	// the extraction order must be SAPEXE_403 before dw_421, in every kernel directory
	var order []string
	for _, c := range fake.Calls {
		if filepath.Base(c.Path) == "SAPCAR" {
			order = append(order, filepath.Base(c.Dir)+"/"+filepath.Base(c.Args[1]))
		}
	}
	if strings.Join(order, " ") != "linuxx86_64/SAPEXE_403-1.SAR linuxx86_64/dw_421-1.sar exe/SAPEXE_403-1.SAR exe/dw_421-1.sar exe/SAPEXE_403-1.SAR exe/dw_421-1.sar" {
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
	backups := map[string]string{}
	for _, d := range bk.Dirs {
		backups[d.Source] = d.Dest
	}
	if _, err := Restore(context.Background(), e, backups); err != nil {
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

func TestHomeDirs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"home/tcxxx", "export/home/basis", "home/abcadm"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	passwd := filepath.Join(root, "passwd")
	os.WriteFile(passwd, []byte(strings.Join([]string{
		"root:x:0:0:root:/:/bin/sh",
		"daemon:x:1:1::/usr/sbin:/usr/sbin/nologin",
		"tcxxx:x:1001:100:Basis:" + filepath.Join(root, "home/tcxxx") + ":/bin/bash",
		"basis:x:1002:100::" + filepath.Join(root, "export/home/basis") + ":/bin/ksh",
		"abcadm:x:1003:79::" + filepath.Join(root, "home/abcadm") + ":/bin/csh",
		"gone:x:1004:100::" + filepath.Join(root, "home/gone") + ":/bin/sh", // does not exist
		"dup:x:1005:100::" + filepath.Join(root, "home/tcxxx") + ":/bin/sh", // same home twice
		"broken line",
	}, "\n")), 0o644)
	prevPW, prevHome := PasswdFile, os.Getenv("HOME")
	PasswdFile = passwd
	os.Setenv("HOME", filepath.Join(root, "home/abcadm"))
	defer func() { PasswdFile = prevPW; os.Setenv("HOME", prevHome) }()
	got := HomeDirs()
	want := []string{filepath.Join(root, "home/tcxxx"), filepath.Join(root, "export/home/basis"), filepath.Join(root, "home/abcadm")}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("HomeDirs = %v\nwant %v", got, want)
	}
}

func TestIsSAPCARName(t *testing.T) {
	for name, want := range map[string]bool{"SAPCAR": true, "sapcar": true, "SAPCAR.exe": true, "SAPCAR_1115-70006178.EXE": true,
		"sapcar-1010.exe": true, "SAPCAR_721.EXE": true, "SAPCARS": false, "sapcar.txt": false, "SAPEXE_403.SAR": false, "mysapcar": false} {
		if IsSAPCARName(name) != want {
			t.Errorf("IsSAPCARName(%q) = %v", name, !want)
		}
	}
	if SAPCARPatch("SAPCAR_1115-70006178.EXE") != 1115 || SAPCARPatch("SAPCAR") != 0 {
		t.Error("patch number")
	}
}

func TestFindSAPCAR(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home", "root"))
	prevBin, prevRoots := ProgramBinDir, DefaultScanRoots
	ProgramBinDir = func() string { return filepath.Join(root, "opt", "kernelman", "bin") }
	DefaultScanRoots = []string{root}
	defer func() { ProgramBinDir, DefaultScanRoots = prevBin, prevRoots }()
	os.MkdirAll(ProgramBinDir(), 0o755)
	kdir := filepath.Join(root, "sapmnt", "ABC", "exe", "uc", "linuxx86_64")
	dl := filepath.Join(root, "home", "tcxxx", "Downloads")
	os.MkdirAll(kdir, 0o755)
	os.MkdirAll(dl, 0o755)
	e := &Env{T: &system.Target{SID: "ABC", KernelDir: kdir, KernelDirs: []string{kdir}}, Pr: &recorder{}}

	// 1. nothing anywhere → a clear error naming the places
	if _, err := FindSAPCAR(context.Background(), e, []string{dl}, nil); err == nil || !strings.Contains(err.Error(), "SAP Software Center") {
		t.Fatalf("err = %v", err)
	}
	// 2. an uploaded, lower-case, non-executable copy in the download directory: used, chmod 755, copies kept
	up := filepath.Join(dl, "sapcar_1115-70006178.exe")
	os.WriteFile(up, []byte("#!/bin/sh\nexit 0\n"), 0o644)
	got, err := FindSAPCAR(context.Background(), e, []string{dl}, nil)
	if err != nil || got != up {
		t.Fatalf("got %q, %v", got, err)
	}
	if info, _ := os.Stat(up); info.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", info.Mode())
	}
	for _, p := range []string{filepath.Join(ProgramBinDir(), "SAPCAR"), filepath.Join(StateBinDir(), "SAPCAR")} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("copy %s: %v", p, err)
		}
	}
	// 3. next time the copy next to KernelMan wins, whatever the download directory holds
	if got, _ := FindSAPCAR(context.Background(), e, []string{dl}, nil); got != filepath.Join(ProgramBinDir(), "SAPCAR") {
		t.Errorf("got %q", got)
	}
	// 4. only a copy somewhere odd on the server: the whole-server walk finds it (any case)
	os.RemoveAll(ProgramBinDir())
	os.RemoveAll(StateBinDir())
	os.Remove(up)
	odd := filepath.Join(root, "opt", "tools", "SapCar")
	os.MkdirAll(filepath.Dir(odd), 0o755)
	os.WriteFile(odd, []byte("x"), 0o755)
	if got, err := FindSAPCAR(context.Background(), e, nil, nil); err != nil || got != odd {
		t.Errorf("got %q, %v", got, err)
	}
}
