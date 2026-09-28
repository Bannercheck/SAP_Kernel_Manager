package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/download"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ship"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/swdc"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// router sends SAP binaries to the fake and cp/ls/chown to the real runner.
type router struct {
	fake *exec.Fake
	real exec.Runner
}

var fakeBins = map[string]bool{"sapcontrol": true, "SAPCAR": true, "disp+work": true, "saphostctrl": true, "saphostexec": true, "hostexecstart": true, "sapstartsrv": true, "ssh": true, "scp": true}

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
	root      string
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
	d00 := filepath.Join(root, "usr", "sap", "ABC", "D00", "exe")
	ascs := filepath.Join(root, "usr", "sap", "ABC", "ASCS01", "exe")
	dl := filepath.Join(root, "home", "tcxxx", "Downloads") // uploaded into a personal home directory
	for _, d := range []string{kdir, d00, ascs, dl} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{d00, ascs} {
		for _, f := range []string{"sapstartsrv", "sapcontrol", "disp+work"} {
			if err := os.WriteFile(filepath.Join(d, f), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
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
		On(sc+"00 -function WaitforStarted 900 10", okBody("WaitforStarted"), 0).On(sc+"01 -function WaitforStarted 900 10", okBody("WaitforStarted"), 0).
		On(sc+"00 -function ParameterValue dbms/type", "\n27.09.2026 10:00:00\nParameterValue\nOK\nhdb\n", 0)
	sapcar := filepath.Join(kdir, "SAPCAR")
	for _, dir := range []string{kdir, d00, ascs} {
		for _, n := range []string{"SAPEXE_403-80007807.SAR", "SAPEXEDB_403-80007808.SAR", "dw_421-80007541.sar", "dw_423-80007541.sar"} {
			out := "x disp+work\nx gwrd\nx icman\nx msg_server\nx sapstartsrv\nx libsapu16.so\n"
			if strings.HasPrefix(n, "dw_") {
				out = "x disp+work\n"
			}
			f.On(sapcar+" -xvf "+filepath.Join(dir, n), out, 0)
		}
	}
	f.On(filepath.Join(kdir, "disp+work")+" -V", dispworkV(200).Stdout, 0)

	tgt := &system.Target{SID: "ABC", SIDAdm: "abcadm", Group: "sapsys", KernelDir: kdir, KernelDirs: []string{kdir, d00, ascs}, DirExeRoot: "/usr/sap/ABC/SYS/exe",
		StateDir: filepath.Join(root, "state"), Sapcontrol: "/usr/sap/hostctrl/exe/sapcontrol", Source: "sapcontrol ParameterValue DIR_CT_RUN (instance 00)",
		Instances: []discovery.Instance{{SID: "ABC", Nr: "00", Name: "D00", Type: "D", Host: "sapci", ExeDir: d00}, {SID: "ABC", Nr: "01", Name: "ASCS01", Type: "ASCS", Host: "sapci", ExeDir: ascs}}}
	os.MkdirAll(tgt.StateDir, 0o755)
	return &flowEnv{fake: f, target: tgt, kernelDir: kdir, download: dl, root: root, sc: sc}
}

// install wires the flow environment into the CLI for one test.
func (fe *flowEnv) install(t *testing.T) {
	t.Helper()
	prevRunner, prevResolve, prevCollect, prevRoot, prevScan, prevDefault, prevCT := runner, resolveTarget, collectStatus, isRoot, scanRoots, ops.DefaultScanRoots, ops.ChangeTime
	runner = router{fake: fe.fake, real: exec.NewReal()}
	isRoot = false
	t.Setenv("HOME", fe.root) // ~/.kernelman of the test host
	scanRoots = nil
	ops.DefaultScanRoots = []string{fe.root}                            // the test root stands in for "/"
	ops.ChangeTime = func(os.FileInfo) time.Time { return time.Time{} } // test files are all created now
	transcriptRoot = fe.root
	resolveTarget = func(context.Context, string) (*system.Target, []string, error) {
		fe.target.Snapshot, _ = system.LoadSnapshot(fe.target.StateDir)
		return fe.target, nil, nil
	}
	collectStatus = func(context.Context, status.Options) *status.Report { return exampleReport() }
	t.Cleanup(func() {
		runner, resolveTarget, collectStatus, isRoot, scanRoots, ops.DefaultScanRoots, ops.ChangeTime = prevRunner, prevResolve, prevCollect, prevRoot, prevScan, prevDefault, prevCT
		transcriptRoot = ""
	})
}

// transcriptRoot is the flow environment's root; transcripts show it as "/".
var transcriptRoot string

// runMenu drives one menu session and optionally records the transcript.
func runMenu(t *testing.T, name, input string) string {
	t.Helper()
	pal := ui.Palette{Colour: true, Unicode: true}
	var out bytes.Buffer
	Menu(strings.NewReader(input), &out, pal)
	if os.Getenv("KERNELMAN_WRITE_EXAMPLE") == "1" && name != "" {
		screen := out.String()
		if transcriptRoot != "" {
			screen = strings.ReplaceAll(strings.ReplaceAll(screen, transcriptRoot+"/", "/"), transcriptRoot, "/")
		}
		content := "$ ./kernelman.sh              # menü · " + strings.ReplaceAll(strings.TrimSpace(input), "\n", " ⏎ ") + "\n" + screen
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
	out := runMenu(t, "menu", "1\nm\nq\n")
	mustContain(t, out, "1) ✔ SAP Status", "SYSTEM ABC · AS ABAP · Hostname sapci", "[M] Main menu  [Q] Quit")
}

func TestFlowBackup(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	out := runMenu(t, "backup", "2\ny\nm\nq\n") // 2, Y = yes, M = main menu, quit
	want := filepath.Join(filepath.Dir(fe.kernelDir), "linuxx86_64_"+time.Now().Format("20060102"))
	mustContain(t, out, "[1/11] Check free space ... ok", "[3/11] Copy to "+want, "[4/11] Verify linuxx86_64_", "9 files match", "Backup "+want, "disp+work",
		"exe_"+time.Now().Format("20060102"), "Backup ready: 3 directories, 15 files", "2) ✔ Kernel Backup")
	if _, err := os.Stat(filepath.Join(want, "gwrd")); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

func TestFlowFiles(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	out := runMenu(t, "files", "4\ny\nm\nq\n") // 4, Y = all, M = main menu, quit (no directory question: the server is scanned)
	mustContain(t, out, "=== Kernel File Transfer ===", "scanning the whole server ("+fe.root+") including every user's home directory for .SAR files placed today", "Archives placed on this server today, in apply order", "1  SAPEXE_403-80007807.SAR", "4  dw_423-80007541.sar",
		"target level after apply: patch 423", "2 older archive(s) ignored", "[1/8] Check permissions of 4 archive(s) in "+fe.download+" ... ok  chmod SAPEXE_403-80007807.SAR 0644 → 0755",
		"[2/8] Copy 4 archive(s)", "[6/8] Copy 4 archive(s)",
		"4 archive(s) copied into 3 kernel directories", "4) ✔ Kernel File Transfer")
	for _, dir := range fe.target.KernelDirs {
		for _, n := range []string{"SAPEXE_403-80007807.SAR", "dw_423-80007541.sar"} {
			if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
				t.Errorf("%s not copied to %s", n, dir)
			}
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
	out := runMenu(t, "stop", "5\nk\nm\nq\n") // 5, K = stop, M = main menu, quit
	mustContain(t, out, "[S] Start SAP  [K] Stop SAP (Kapat)  [M] Main menu", "⬤ RUNNING", "[1/5] StopSystem ALL ... ok",
		"[5/5] StopService ASCS01 (01) ... ok", "system ABC stopped", "⬤ STOPPED", "D00 (sapstartsrv down)", "5) ✔ SAP Stop / Start")
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

	out := runMenu(t, "update", "6\ny\ny\nm\nq\n") // 6, Y = confirm order, Y = start afterwards, M = main menu, quit
	mustContain(t, out, "backup from today", "scanning the whole server ("+fe.root+")", "1  SAPEXE_403-80007807.SAR", "FOUND IN", fe.download, "[1/17] SAPCAR -xvf SAPEXE_403-80007807.SAR  (SAPEXE 403) in "+fe.kernelDir+" ... ok  6 files",
		"[4/17] SAPCAR -xvf dw_423-80007541.sar  (dw 423) in "+fe.kernelDir+" ... ok  1 files", "[5/17] SAPCAR -xvf SAPEXE_403-80007807.SAR  (SAPEXE 403) in "+fe.target.KernelDirs[1],
		"[17/17] Read kernel version (disp+work -V) ... ok  793 Patch 423",
		"Kernel ABC: 793 Patch 200 → 793 Patch 423", "[3/5] StartSystem ALL ... ok", "system ABC started", "⬤ RUNNING", "6) ✔ Kernel Update")
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
	out := runMenu(t, "rollback", "7\n\ny\nn\nm\nq\n") // 7, backup default (Enter keeps it), Y = confirm, N = no start, M = main menu, quit
	mustContain(t, out, "Backup for "+fe.kernelDir+" ["+bk.Dirs[0].Dest+"]", "[1/8] Copy "+bk.Dirs[0].Dest, "[2/8] Copy "+bk.Dirs[1].Dest,
		"Kernel ABC restored: 793 Patch 423 → 793 Patch 200", "7) ✔ Kernel Rollback")
	b, _ := os.ReadFile(filepath.Join(fe.kernelDir, "gwrd"))
	if string(b) == "broken" {
		t.Error("gwrd not restored")
	}
}

// fakeSoftwareCenter serves a catalogue (already signed in: the probe gets
// JSON at once) and the files behind the links, like SAP for Me plus
// softwaredownloads.sap.com on one host.
func fakeSoftwareCenter(t *testing.T) *httptest.Server {
	t.Helper()
	const body = "CAR 2.01\x00fake kernel archive for the transcript ......................................................"
	mux := http.NewServeMux()
	mux.HandleFunc("/services/odata/svt/swdcuisrv/SearchResultSet", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		q := strings.ToUpper(strings.Fields(r.URL.Query().Get("SEARCH_STRING"))[0])
		type row = map[string]any
		var rows []row
		add := func(title, desc, info, key, size string) {
			if strings.Contains(strings.ToUpper(title), q) {
				rows = append(rows, row{"Title": title, "Description": desc, "Infotype": info, "Fastkey": key,
					"DownloadDirectLink": "http://" + r.Host + "/file/" + key, "Filesize": size, "ChangeDate": "20260915"})
			}
		}
		add("SAPEXE_400-80007807.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000400", "1234567")
		add("SAPEXE_403-80007807.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000403", "1234567")
		add("SAPEXE_403-80007900.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "AIX 64bit | #DATABASE INDEPENDENT", "0020000000000499", "1234567")
		add("SAPEXEDB_403-80007808.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | SAP HANA DATABASE", "0020000000000413", "234567")
		add("SAPEXEDB_403-80007809.SAR", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | ORACLE", "0020000000000414", "234567")
		add("dw_421-80007541.sar", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000421", "45678")
		add("dw_423-80007541.sar", "SAP KERNEL 7.93 64-BIT UNICODE", "Linux on x86_64 64bit | #DATABASE INDEPENDENT", "0020000000000423", "45678")
		json.NewEncoder(w).Encode(map[string]any{"d": map[string]any{"results": rows}})
	})
	names := map[string]string{"0020000000000403": "SAPEXE_403-80007807.SAR", "0020000000000413": "SAPEXEDB_403-80007808.SAR",
		"0020000000000421": "dw_421-80007541.sar", "0020000000000423": "dw_423-80007541.sar"}
	mux.HandleFunc("/file/", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "S0001234567" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+names[filepath.Base(r.URL.Path)]+`"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

// installDownload points the download and Software Center clients at srv.
func installDownload(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	prevClient, prevSession, prevDemo, prevReach := newDownloadClient, newSWDCSession, demoRoot, reachabilityURL
	reachabilityURL = srv.URL + "/"
	newDownloadClient = func(c download.Credentials) *download.Client {
		cl := download.NewClient(c)
		cl.TrustedHosts, cl.Retries = []string{"127.0.0.1"}, 0
		return cl
	}
	newSWDCSession = func(c download.Credentials) *swdc.Session {
		s := swdc.New(c.User, c.Password)
		s.Launchpad, s.AllowedHosts = srv.URL, []string{"127.0.0.1"}
		return s
	}
	demoRoot = t.TempDir() // credentials, log and default target live under a scratch root
	t.Cleanup(func() {
		newDownloadClient, newSWDCSession, demoRoot, reachabilityURL = prevClient, prevSession, prevDemo, prevReach
	})
	return demoRoot
}

// TestFlowDownload drives the automatic Kernel Download: sign in, find the
// archives for kernel 793 / linuxx86_64 / HANA, confirm, download.
func TestFlowDownload(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	srv := fakeSoftwareCenter(t)
	defer srv.Close()
	root := installDownload(t, srv)
	target := filepath.Join(root, "download")
	// 3, S-user, password, remember password? N, target dir (Enter = default), Y = download, M, q
	out := runMenu(t, "download", "3\nS0001234567\nsecret\nn\n\ny\nm\nq\n")
	mustContain(t, out, "=== Kernel Download ===", "checking access to SAP ... ok", "S-user: S0001234567", "Password for S0001234567: ******",
		"looking for: kernel 793 · linuxx86_64 · HDB · current patch 200", "signing in to SAP for Me as S0001234567 ... ok",
		"Proposed download for kernel 793 (stack 403 → target patch 423)",
		"1  SAPEXE_403-80007807.SAR", "2  SAPEXEDB_403-80007808.SAR", "3  dw_421-80007541.sar", "4  dw_423-80007541.sar",
		"other platforms ignored", "other database", "Download these 4 file(s) as S0001234567?",
		"[1/4] SAPEXE_403-80007807.SAR ... ", "ok SAPEXE_403-80007807.SAR", "sha256", "[4/4] dw_423-80007541.sar",
		"4 file(s), ", "dated today, so Kernel File Transfer will find them", "3) ✔ Kernel Download")
	for _, n := range []string{"SAPEXE_403-80007807.SAR", "SAPEXEDB_403-80007808.SAR", "dw_421-80007541.sar", "dw_423-80007541.sar"} {
		if !download.IsSAR(filepath.Join(target, n)) {
			t.Errorf("%s missing or not a SAR", n)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, ".kernelman", "swdc.log")); err != nil || !strings.Contains(string(b), "search") || strings.Contains(string(b), "secret") {
		t.Errorf("swdc.log: %v %q", err, string(b))
	}
	if c, ok, _ := download.LoadCredentials(filepath.Join(root, ".kernelman")); !ok || c.User != "S0001234567" || c.Password != "" {
		t.Errorf("credentials: %+v %v", c, ok)
	}
}

// TestDownloadBasketCommandLine covers the fallback: links from a basket
// export, no questions, password from the environment.
func TestDownloadBasketCommandLine(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	srv := fakeSoftwareCenter(t)
	defer srv.Close()
	root := installDownload(t, srv)
	basket := filepath.Join(t.TempDir(), "DownloadBasket.txt")
	os.WriteFile(basket, []byte(srv.URL+"/file/0020000000000423\tdw_423-80007541.sar\tdisp+work\t45 MB\n"), 0o644)
	t.Setenv("KERNELMAN_SUSER_PASSWORD", "secret")
	var out bytes.Buffer
	prev := stdout
	stdout = &out
	defer func() { stdout = prev }()
	if code := DownloadOp([]string{"--basket", basket, "--to", filepath.Join(root, "dl"), "--user", "S0001234567", "--yes"}); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if !download.IsSAR(filepath.Join(root, "dl", "dw_423-80007541.sar")) {
		t.Errorf("file not downloaded:\n%s", out.String())
	}
}

// TestDownloadOffline: without access to SAP the operation explains itself
// and stops before asking for credentials.
func TestDownloadOffline(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	prev := reachabilityURL
	reachabilityURL = "http://127.0.0.1:1/" // nothing listens here
	defer func() { reachabilityURL = prev }()
	out := runMenu(t, "download-offline", "3\nm\nq\n")
	mustContain(t, out, "this host cannot reach SAP", "Kernel Download is optional", "3) ✘ Kernel Download")
	if strings.Contains(out, "S-user:") {
		t.Error("credentials must not be asked when offline")
	}
}

// TestFlowShip sends today's archives and a KernelMan distribution to two
// hosts; ssh/scp are faked, cksum runs for real on the local files.
func TestFlowFilesSelect(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	// 4, S = select, a bad answer, then 1-2 and 4, Y, M, quit
	out := runMenu(t, "files-select", "4\ns\n9\n1-2,4\ny\nm\nq\n")
	mustContain(t, out, "[Y] Yes, all  [S] Select  [N] No", `"9" is not a number or range between 1 and 4`,
		"Selected 3 of 4 archive(s), in apply order", "3  dw_423-80007541.sar", "target level after apply: patch 423",
		"Copy these 3 archive(s) into 3 kernel directories?", "[2/8] Copy 3 archive(s)", "3 archive(s) copied into 3 kernel directories")
	for _, dir := range fe.target.KernelDirs {
		if _, err := os.Stat(filepath.Join(dir, "dw_421-80007541.sar")); err == nil {
			t.Errorf("unselected archive copied to %s", dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "dw_423-80007541.sar")); err != nil {
			t.Errorf("selected archive missing in %s", dir)
		}
	}
}

func TestParseSelection(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		err  bool
	}{
		{"1,3-4", "1 3 4", false}, {"2", "2", false}, {"4-4 1 1", "1 4", false}, {"1;2", "1 2", false},
		{"0", "", true}, {"5", "", true}, {"3-2", "", true}, {"a", "", true}, {"", "", true},
	} {
		got, err := parseSelection(tc.in, 4)
		if (err != nil) != tc.err || (err == nil && fmt.Sprint(got) != "["+tc.want+"]") {
			t.Errorf("parseSelection(%q) = %v, %v", tc.in, got, err)
		}
	}
}

func TestFlowServices(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	// SAP Host Agent stopped and ASCS01's sapstartsrv down for the home screen and the first look; up afterwards
	calls := 0
	collectStatus = func(context.Context, status.Options) *status.Report {
		calls++
		rep := exampleReport()
		rep.Systems = rep.Systems[:1]
		if calls <= 2 {
			rep.HostAgent.Running, rep.HostAgent.Processes = false, nil
			rep.Systems[0].Instances[1].Sapstartsrv = "not running"
		}
		return rep
	}
	isRoot = true
	ha := "/usr/sap/hostctrl/exe/saphostexec"
	fe.fake.Paths["saphostexec"] = ha
	fe.fake.On(ha+" -restart", "", 0).OnSeq(ha+" -status", exec.Result{Stdout: "saphostexec running (pid = 4242)\nsapstartsrv running (pid = 4243)\nsaposcol running (pid = 4244)\n"})
	fe.fake.On(ha+" -version", "kernel release                722\n\npatch number                  65\n", 0)
	// sapcontrol StartService fails (sapstartsrv never ran with the profile) → sapstartsrv pf=<profile> -D -u abcadm
	fe.fake.On(fe.sc+"01 -function StartService ABC", "\n28.09.2026 10:00:00\nStartService\nFAIL: NIECONN_REFUSED (Connection refused)\n", 1)
	fe.fake.OnSeq(fe.sc+"01 -function GetProcessList", procs("GRAY", 4))
	ascs := fe.target.Instances[1].ExeDir
	fe.fake.On(ascs+"/sapstartsrv pf=/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci -D -u abcadm", "", 0)
	out := runMenu(t, "services", "9\na\nm\nq\n") // 9, A = start all, M, quit
	mustContain(t, out, "=== SAP Services ===", "SAP Host Agent   ⬤ STOPPED", "● saphostexec not running   ● sapstartsrv not running   ● saposcol not running",
		"ABC ASCS01  ● not running  /usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci",
		"[H] Start SAP Host Agent  [S] Start sapstartsrv (1 instance(s))  [A] Start all  [M] Main menu",
		"[1/2] Start SAP Host Agent ... ok  saphostexec -restart · saphostexec, sapstartsrv, saposcol running",
		"[2/2] Start sapstartsrv ASCS01 (01) ... ok  StartService failed", "→ sapstartsrv pf=/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci -D · answers",
		"SAP Host Agent   ⬤ RUNNING", "● saphostexec running (pid = 4242)   ● sapstartsrv running (pid = 4243)   ● saposcol running (pid = 4244)",
		"ABC ASCS01  ● running", "SAP Host Agent and every sapstartsrv are running · sapcontrol works again", "9) ✔ SAP Services")
	var started bool
	for _, c := range fe.fake.Calls {
		if strings.HasSuffix(c.Path, "/sapstartsrv") && len(c.Env) == 1 && strings.HasSuffix(c.Env[0], "="+ascs) {
			started = true
		}
	}
	if !started {
		t.Errorf("sapstartsrv was not started with the instance library path")
	}
}

func TestFlowShip(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	prevCD := ship.ControlDir
	ship.ControlDir = func() string { return "" } // fixed command lines for the fake
	t.Cleanup(func() { ship.ControlDir = prevCD })
	// a dist layout to ship: <dir>/kernelman.sh + <dir>/bin/kernelman-linux-amd64
	dist := filepath.Join(t.TempDir(), "kernelman")
	os.MkdirAll(filepath.Join(dist, "bin"), 0o755)
	os.WriteFile(filepath.Join(dist, "kernelman.sh"), []byte(ship.Launcher()), 0o755)
	os.WriteFile(filepath.Join(dist, "bin", "kernelman-linux-amd64"), []byte("ELF fake"), 0o755)
	prevLocate := locateProgram
	locateProgram = func(string, ...string) (ship.Program, error) {
		return ship.Program{Dir: dist, Files: []string{"kernelman.sh", "bin/kernelman-linux-amd64"}}, nil
	}
	t.Cleanup(func() { locateProgram = prevLocate })

	today, _, _ := ops.ScanSARs(fe.download, time.Now())
	var localPaths, remotePaths []string
	for _, a := range today {
		localPaths = append(localPaths, a.Path)
		remotePaths = append(remotePaths, "/usr/sap/download/"+a.Name)
	}
	for _, f := range []string{"kernelman.sh", "bin/kernelman-linux-amd64"} {
		localPaths = append(localPaths, filepath.Join(dist, f))
		remotePaths = append(remotePaths, "/usr/sap/download/kernelman/"+f)
	}
	res, _ := exec.NewReal().Run(context.Background(), exec.Cmd{Path: "cksum", Args: localPaths})
	remoteOut := res.Stdout
	for i, lp := range localPaths {
		remoteOut = strings.ReplaceAll(remoteOut, lp, remotePaths[i])
	}
	sorted := append([]string{}, remotePaths...)
	sort.Strings(sorted)
	opts := "-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "
	for _, host := range []string{"app2", "app3"} {
		ssh := "ssh " + opts + "abcadm@" + host + " "
		fe.fake.On(ssh+"uname -sn", "Linux "+host+"\n", 0).
			On(ssh+"mkdir -p /usr/sap/download/kernelman", "", 0).
			On("scp -p -r "+opts+strings.Join(localPaths[:len(today)], " ")+" abcadm@"+host+":/usr/sap/download/", "", 0).
			On("scp -p -r "+opts+dist+"/bin "+dist+"/kernelman.sh abcadm@"+host+":/usr/sap/download/kernelman/", "", 0).
			On(ssh+"chmod -R u+x /usr/sap/download/kernelman/kernelman.sh /usr/sap/download/kernelman/bin", "", 0).
			On(ssh+"cksum "+strings.Join(sorted, " "), remoteOut, 0).
			On(ssh+"ls -la /usr/sap/download", "total 8\ndrwxr-xr-x 3 abcadm sapsys 4096 Sep 27 22:40 kernelman\n-rw-r--r-- 1 abcadm sapsys 4096 Sep 27 22:40 SAPEXE_403-80007807.SAR\n", 0)
	}
	// 8, include archives Y, hosts, login (Enter = abcadm), remote dir (Enter), send Y, M, q
	out := runMenu(t, "ship", "8\ny\napp2, app3\n\n\ny\nm\nq\n")
	mustContain(t, out, "=== Send to Other Servers ===", "Include these 4 archive(s) in the shipment?", "Target servers", "Remote login [abcadm]",
		"archives   4 file(s)", "to         abcadm@{app2,app3}:/usr/sap/download", "── abcadm@app2 ──", "[1/5] Connect to app2 ... ok  Linux app2",
		"[3/5] Copy 4 archive(s) to app2:/usr/sap/download", "[4/5] Copy KernelMan to app2:/usr/sap/download/kernelman", "[5/5] Verify checksums on app2 ... ok  6 files match",
		"── abcadm@app3 ──", "app2  ✔ sent", "app3  ✔ sent", "On each server: cd /usr/sap/download/kernelman && ./kernelman.sh", "8) ✔ Send to Other Servers")
}

// Transcripts must show the typed answers whether or not the test runs from a terminal.
func init() { echoInput = true }
