package hub

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	kexec "github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

func TestMain(m *testing.M) {
	pbkdfIter = 1000 // keep the tests fast; production uses 600 000
	os.Exit(m.Run())
}

func TestAuthAndVault(t *testing.T) {
	if _, err := NewAuth("admin", "short"); err == nil {
		t.Fatal("short password accepted")
	}
	a, err := NewAuth("admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	key, ok := a.Verify("admin", "correct horse battery")
	if !ok || len(key) != 32 {
		t.Fatalf("verify failed: %v %d", ok, len(key))
	}
	if _, ok := a.Verify("admin", "wrong password!!"); ok {
		t.Error("wrong password accepted")
	}
	if _, ok := a.Verify("root", "correct horse battery"); ok {
		t.Error("wrong user accepted")
	}
	sealed, err := Seal(key, "s3cret-ssh")
	if err != nil || sealed == "" || strings.Contains(sealed, "s3cret") {
		t.Fatalf("seal: %q %v", sealed, err)
	}
	if plain, err := Open(key, sealed); err != nil || plain != "s3cret-ssh" {
		t.Errorf("open: %q %v", plain, err)
	}
	other, _ := NewAuth("admin", "another password")
	k2, _ := other.Key("another password")
	if _, err := Open(k2, sealed); err == nil {
		t.Error("opened with a different key")
	}
	if _, err := Seal(nil, "x"); err == nil {
		t.Error("sealed while locked")
	}
}

func TestCksumMatchesPOSIX(t *testing.T) {
	data := strings.Repeat("SAP kernel archive \x00\xff ", 5000)
	var c cksum
	c.Write([]byte(data))
	if _, err := exec.LookPath("cksum"); err != nil {
		t.Skip("no cksum on this machine")
	}
	cmd := exec.Command("cksum")
	cmd.Stdin = strings.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := parseCksumLine(string(out)); c.Sum() != want {
		t.Errorf("cksum = %q, POSIX cksum says %q", c.Sum(), want)
	}
}

func TestStoreValidation(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []Host{{Name: "", User: "root"}, {Name: "a b", User: "root"}, {Name: "h", User: "x;rm"}, {Name: "h;id", User: "root"}, {Name: "h", User: "$(id)"},
		{Name: "h", User: "u", Become: "admin"},
		{Name: "h", User: "u", SudoPW: true}} {
		if _, err := s.PutHost(h); err == nil {
			t.Errorf("accepted %+v", h)
		}
	}
	if _, err := s.PutHost(Host{Name: "10.20.30.40", User: "tayfur@corp.example.com", Become: "root"}); err != nil {
		t.Errorf("domain login refused: %v", err)
	}
	h, err := s.PutHost(Host{Name: "sapdev01", User: "devadm", Become: "sidadm", SudoPW: true})
	if err != nil || h.ID == "" || h.Port != 22 || h.Auth != "password" {
		t.Fatalf("put: %+v %v", h, err)
	}
	if _, err := s.PutHost(Host{Name: "SAPDEV01", User: "x"}); err == nil {
		t.Error("duplicate host accepted")
	}
	if err := s.PutSettings(Settings{RemoteDir: "/tmp/x; rm -rf /"}); err == nil {
		t.Error("shell characters accepted in the remote directory")
	}
	if _, open := s.Unlocked("DEV"); open {
		t.Error("a system starts unlocked")
	}
	if _, err := s.SetLock("dev", true); err != nil {
		t.Fatal(err)
	}
	if until, open := s.Unlocked("DEV"); !open || until.Sub(time.Now()) < 7*time.Hour {
		t.Errorf("unlock: %v %v", until, open)
	}
	s.SetLock("DEV", false)
	if _, open := s.Unlocked("DEV"); open {
		t.Error("lock did not close the system")
	}
	s2, err := OpenStore(s.Dir()) // persisted
	if err != nil || len(s2.Hosts()) != 2 {
		t.Errorf("reload: %v %d", err, len(s2.Hosts()))
	}
}

func TestAgentCmdRunAs(t *testing.T) {
	ri := remoteInfo{Agent: "/var/tmp/kernelman-tayfur/kernelman"}
	cases := []struct {
		host      Host
		sid       string
		want      string
		wantStdin bool
	}{
		{Host{User: "devadm"}, "DEV", "exec \"$@\"' km env KERNELMAN_COLOR=never KERNELMAN_UNICODE=1 /var/tmp/kernelman-tayfur/kernelman stop --sid DEV", false},
		{Host{User: "tayfur", Become: "sidadm"}, "DEV", "km sudo -n -H -u devadm env", false},
		{Host{User: "tayfur", Become: "sidadm", SudoPW: true}, "DEV", "km sudo -S -p '' -H -u devadm env", true},
		{Host{User: "tayfur", Become: "root", SudoPW: true}, "", "km sudo -S -p '' env", true},
		{Host{User: "tayfur", Become: "sidadm"}, "", "km env KERNELMAN", false}, // status reads stay with the login user
	}
	for _, c := range cases {
		cmd, stdin := agentCmd(Target{Host: c.host, Password: "pw"}, ri, c.sid, "stop", "--sid", "DEV")
		if !strings.Contains(cmd, c.want) || (stdin != nil) != c.wantStdin {
			t.Errorf("%+v: %s (stdin %v)", c.host, cmd, stdin != nil)
		}
		if strings.Contains(cmd, "pw") {
			t.Errorf("password on the command line: %s", cmd)
		}
	}
}

func TestSplitQuotedRoundTrip(t *testing.T) {
	args := []string{"/bin/sh", "-c", `d="$1"; echo "$d"`, "km", "it's", "a b", "plain"}
	var parts []string
	for _, a := range args {
		parts = append(parts, kexec.ShellQuote(a))
	}
	got := splitQuoted(strings.Join(parts, " "))
	if strings.Join(got, "|") != strings.Join(args, "|") {
		t.Errorf("got %q", got)
	}
}

// demoHub is a hub over the simulated landscape, refreshed and logged in.
func demoHub(t *testing.T) (*Hub, *DemoRemote) {
	t.Helper()
	r := NewDemoRemote()
	r.Delay = 0
	h, err := New(Config{DataDir: t.TempDir(), Remote: r, Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetupDemo(h, "admin", "kernelman1"); err != nil {
		t.Fatal(err)
	}
	return h, r
}

func TestPlanSafetyRules(t *testing.T) {
	h, _ := demoHub(t)
	if _, err := h.Plan(JobRequest{Kind: "backup", SID: "PRD"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("locked system: %v", err)
	}
	h.Store.SetLock("PRD", true)
	if _, err := h.Plan(JobRequest{Kind: "stop", SID: "PRD"}); err == nil || !strings.Contains(err.Error(), "type the SID") {
		t.Errorf("stop without confirmation: %v", err)
	}
	j, err := h.Plan(JobRequest{Kind: "update", SID: "PRD", Confirm: "prd", Start: true,
		Archives: []string{"SAPEXE_423-80008765.SAR", "SAPEXEDB_423-80008766.SAR"}})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, s := range j.Steps {
		order = append(order, s.Kind+":"+s.HostName)
	}
	got := strings.Join(order, " ")
	want := "preflight:prdascs preflight:prdapp1 preflight:prdapp2 preflight:prdapp3 push:prdascs push:prdapp1 push:prdapp2 push:prdapp3 " +
		"agent:prdascs agent:prdapp1 agent:prdapp2 agent:prdapp3 agent:prdascs agent:prdapp1 agent:prdapp2 agent:prdapp3 verify:prdascs " +
		"agent:prdascs agent:prdapp1 agent:prdapp2 agent:prdapp3"
	if got != want {
		t.Errorf("order:\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(j.Title, "patch 423") {
		t.Errorf("title %q", j.Title)
	}
	// an application server that is not in the hub blocks a system-wide update
	for _, host := range h.Store.Hosts() {
		if host.Name == "prdapp2" {
			h.Store.DeleteHost(host.ID)
			h.forget(host.ID)
		}
	}
	h.RefreshAll(context.Background())
	if _, err := h.Plan(JobRequest{Kind: "update", SID: "PRD", Confirm: "PRD"}); err == nil || !strings.Contains(err.Error(), "prdapp2") {
		t.Errorf("unmanaged host: %v", err)
	}
}

func waitJob(t *testing.T, j *Job) JobInfo {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s := j.Summary(); s.State != "queued" && s.State != "running" {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", j.ID)
	return JobInfo{}
}

func TestUpdateJobEndToEnd(t *testing.T) {
	h, _ := demoHub(t)
	h.Store.SetLock("PRD", true)
	if s, _ := h.System("PRD"); len(s.Warnings) == 0 {
		t.Fatal("the demo should start with prdapp3 behind the central kernel")
	}
	j, err := h.Plan(JobRequest{Kind: "update", SID: "PRD", Confirm: "PRD", Start: true,
		Archives: []string{"SAPEXE_423-80008765.SAR", "SAPEXEDB_423-80008766.SAR", "SAPCAR_1324-70006178.EXE"}})
	if err != nil {
		t.Fatal(err)
	}
	h.Jobs.Submit(j)
	info := waitJob(t, j)
	lines, _ := j.LinesFrom(0)
	var log strings.Builder
	for _, l := range lines {
		log.WriteString(l.Host + " " + l.Text + "\n")
	}
	if info.State != "ok" {
		t.Fatalf("state %s\n%s", info.State, log.String())
	}
	for _, want := range []string{"cksum", "verified", "every instance directory matches the central kernel", "Kernel Update PRD (4 hosts) → patch 423 finished"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("console lacks %q", want)
		}
	}
	h.RefreshAll(context.Background())
	s, _ := h.System("PRD")
	if s.Patch != 423 || s.Status != "GREEN" || len(s.Warnings) != 0 {
		t.Errorf("after the update: patch %d status %s warnings %v", s.Patch, s.Status, s.Warnings)
	}
}

func TestVerifyStopsBeforeStart(t *testing.T) {
	h, _ := demoHub(t)
	h.Store.SetLock("PRD", true)
	// only the central host is updated: prdapp3 (behind) is not part of this job, so check it explicitly
	j, err := h.Plan(JobRequest{Kind: "refresh", SID: "PRD"})
	if err != nil {
		t.Fatal(err)
	}
	var app3 *Step
	for _, s := range j.Steps {
		if s.HostName == "prdapp3" {
			app3 = s
		}
	}
	j.Steps = []*Step{app3}
	err = h.verify(context.Background(), j, app3)
	if err == nil || !strings.Contains(err.Error(), "SAP is not started") || !strings.Contains(err.Error(), "D20") {
		t.Errorf("verify: %v", err)
	}
}

func TestCancelWaitsForTheRunningStep(t *testing.T) {
	h, r := demoHub(t)
	r.Delay = 5 * time.Millisecond
	h.Store.SetLock("QAS", true)
	j, err := h.Plan(JobRequest{Kind: "backup", SID: "QAS"})
	if err != nil {
		t.Fatal(err)
	}
	j.Steps = append(j.Steps, &Step{Host: j.Steps[0].Host, HostName: j.Steps[0].HostName, Kind: "agent", Title: "second", Args: []string{"backup", "--yes", "--sid", "QAS"}, State: "pending"})
	h.Jobs.Submit(j)
	for j.Summary().State != "running" {
		time.Sleep(time.Millisecond)
	}
	if !h.Jobs.Cancel(j.ID) {
		t.Fatal("cancel refused")
	}
	info := waitJob(t, j)
	if info.State != "cancelled" || info.Steps[0].State != "ok" || info.Steps[1].State != "skipped" {
		t.Errorf("state %s steps %s/%s", info.State, info.Steps[0].State, info.Steps[1].State)
	}
}
