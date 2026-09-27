package ship

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// router: ssh/scp go to the fake, cksum runs for real.
type router struct {
	fake *exec.Fake
	real exec.Runner
}

func (r router) Run(ctx context.Context, c exec.Cmd) (exec.Result, error) {
	if b := filepath.Base(c.Path); b == "ssh" || b == "scp" {
		return r.fake.Run(ctx, c)
	}
	return r.real.Run(ctx, c)
}
func (r router) LookPath(f string) (string, error) { return r.real.LookPath(f) }

type recorder struct{ lines []string }

func (r *recorder) Begin(i, n int, title string) {
	r.lines = append(r.lines, fmt.Sprintf("[%d/%d] %s", i, n, title))
}
func (r *recorder) End(err error, detail string, _ time.Duration) {
	if err != nil {
		r.lines = append(r.lines, "  FAIL "+err.Error())
	} else {
		r.lines = append(r.lines, "  ok "+detail)
	}
}
func (r *recorder) Info(line string)         { r.lines = append(r.lines, "  "+line) }
func (r *recorder) Block(title, text string) { r.lines = append(r.lines, "  block "+title) }

func TestLocateAssembles(t *testing.T) {
	p, err := Locate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !p.Created {
		t.Skip("test binary runs from a dist layout")
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "kernelman.sh")); err != nil {
		t.Errorf("launcher missing: %v", err)
	}
	found := false
	for _, f := range p.Files {
		if strings.HasPrefix(f, "bin/kernelman-") {
			found = true
		}
	}
	if !found || len(p.Files) != 2 {
		t.Errorf("files = %v", p.Files)
	}
	if !strings.Contains(Launcher(), "kernelman-$goos-$goarch") {
		t.Error("embedded launcher looks wrong")
	}
}

func TestSendVerifies(t *testing.T) {
	dl := t.TempDir()
	arch := filepath.Join(dl, "dw_423-80007541.sar")
	os.WriteFile(arch, []byte("CAR 2.01 fake"), 0o644)
	prog, err := Locate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := exec.NewReal()
	// what cksum prints locally is what the fake remote will answer, with remote paths
	var localArgs []string
	localArgs = append(localArgs, arch)
	for _, f := range prog.Files {
		localArgs = append(localArgs, filepath.Join(prog.Dir, f))
	}
	res, _ := real.Run(context.Background(), exec.Cmd{Path: "cksum", Args: localArgs})
	remoteOut := res.Stdout
	remoteOut = strings.ReplaceAll(remoteOut, arch, "/usr/sap/download/dw_423-80007541.sar")
	for _, f := range prog.Files {
		remoteOut = strings.ReplaceAll(remoteOut, filepath.Join(prog.Dir, f), "/usr/sap/download/kernelman/"+f)
	}

	f := exec.NewFake()
	sshPrefix := "ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 abcadm@app2 "
	f.On(sshPrefix+"uname -sn", "Linux app2\n", 0).
		On(sshPrefix+"mkdir -p /usr/sap/download/kernelman", "", 0).
		On("scp -p -r -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "+arch+" abcadm@app2:/usr/sap/download/", "", 0).
		On("scp -p -r -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "+prog.Dir+"/. abcadm@app2:/usr/sap/download/kernelman/", "", 0).
		On(sshPrefix+"chmod -R u+x /usr/sap/download/kernelman/kernelman.sh /usr/sap/download/kernelman/bin", "", 0).
		On(sshPrefix+"ls -la /usr/sap/download", "total 3\n-rw-r--r-- 1 abcadm sapsys 13 Sep 27 dw_423-80007541.sar\n", 0)
	var remoteFiles []string
	remoteFiles = append(remoteFiles, "/usr/sap/download/dw_423-80007541.sar")
	for _, pf := range prog.Files {
		remoteFiles = append(remoteFiles, "/usr/sap/download/kernelman/"+pf)
	}
	// Send sorts the remote paths; register the sorted variant
	sorted := append([]string{}, remoteFiles...)
	for i := range sorted { // simple insertion sort to mirror sort.Strings
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	f.On(sshPrefix+"cksum "+strings.Join(sorted, " "), remoteOut, 0)

	rec := &recorder{}
	e := &ops.Env{R: router{fake: f, real: real}, T: &system.Target{SID: "ABC", SIDAdm: "abcadm"}, Pr: rec}
	results := Send(context.Background(), e, Options{Hosts: []string{"app2"}, User: "abcadm", Archives: []ops.SARFile{{Path: arch, Name: "dw_423-80007541.sar"}}, Program: prog})
	if len(results) != 1 || !results[0].OK || results[0].Verified != 1+len(prog.Files) || !strings.Contains(results[0].Listing, "dw_423") {
		t.Fatalf("results = %+v\n%s", results, strings.Join(rec.lines, "\n"))
	}
	joined := strings.Join(rec.lines, "\n")
	for _, want := range []string{"[1/5] Connect to app2", "ok Linux app2", "[3/5] Copy 1 archive(s) to app2:/usr/sap/download", "[4/5] Copy KernelMan", "[5/5] Verify checksums on app2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("progress lacks %q\n%s", want, joined)
		}
	}

	// a host that cannot be reached is reported, not fatal for the others
	bad := exec.NewFake()
	bad.On("ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 abcadm@down uname -sn", "", 255)
	bad.Responses["ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 abcadm@down uname -sn"] = exec.Result{Stderr: "ssh: connect to host down port 22: No route to host", ExitCode: 255}
	e.R = router{fake: bad, real: real}
	results = Send(context.Background(), e, Options{Hosts: []string{"down"}, User: "abcadm", Program: prog})
	if results[0].OK || !strings.Contains(results[0].Err.Error(), "No route to host") {
		t.Errorf("down host: %+v", results[0])
	}
}
