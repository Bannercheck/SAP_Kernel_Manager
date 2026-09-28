package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestShellQuoteAndLine(t *testing.T) {
	if got := ShellQuote("plain-name_1.0"); got != "plain-name_1.0" {
		t.Errorf("safe string quoted: %q", got)
	}
	if got := ShellQuote("it's a test"); got != `'it'\''s a test'` {
		t.Errorf("quote = %q", got)
	}
	line := ShellLine(Cmd{Path: "/usr/sap/ABC/exe/SAPCAR", Args: []string{"-xvf", "SAPEXE_403 (1).SAR"}, Dir: "/sapmnt/ABC/exe", Env: []string{"LIBPATH=/sapmnt/ABC/exe"}})
	want := "cd /sapmnt/ABC/exe && LIBPATH=/sapmnt/ABC/exe /usr/sap/ABC/exe/SAPCAR -xvf 'SAPEXE_403 (1).SAR'"
	if line != want {
		t.Errorf("ShellLine =\n%s\nwant\n%s", line, want)
	}
}

func TestAsUser(t *testing.T) {
	c := Cmd{Path: "sapcontrol", Args: []string{"-nr", "00", "-function", "StartService", "ABC"}, RunAs: "abcadm"}

	root := &Real{IsRoot: true, CurrentUser: "root"}
	got, err := root.asUser(c)
	if err != nil || got.Path != "su" || strings.Join(got.Args, " ") != "- abcadm -c sapcontrol -nr 00 -function StartService ABC" {
		t.Errorf("root: %v %v", got, err)
	}

	same := &Real{CurrentUser: "abcadm"}
	got, err = same.asUser(c)
	if err != nil || got.Path != "sapcontrol" || got.RunAs != "" {
		t.Errorf("same user: %v %v", got, err)
	}

	other := &Real{CurrentUser: "ops", SudoCmd: []string{"sudo", "-n"}}
	got, err = other.asUser(c)
	if err != nil || got.Path != "sudo" || got.Args[0] != "-n" || got.Args[2] != "abcadm" || got.Args[len(got.Args)-2] != "-c" {
		t.Errorf("sudo: %v %v", got, err)
	}

	none := &Real{CurrentUser: "ops"}
	if _, err := none.asUser(c); err == nil {
		t.Error("expected error without sudo")
	}
}

func TestRealRunExitCode(t *testing.T) {
	r := NewReal()
	res, err := r.Run(context.Background(), Cmd{Path: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}})
	if err != nil || res.ExitCode != 3 || strings.TrimSpace(res.Stdout) != "out" || strings.TrimSpace(res.Stderr) != "err" {
		t.Errorf("res=%+v err=%v", res, err)
	}
	if _, err := r.Run(context.Background(), Cmd{Path: "/nonexistent/binary"}); err == nil {
		t.Error("expected not-found error")
	}
}

// A command that leaves a daemon behind (sapstartsrv -D, saphostexec -restart)
// must not block Run until that daemon exits.
func TestRealRunDaemonDoesNotHang(t *testing.T) {
	r := NewReal()
	r.WaitDelay = 300 * time.Millisecond
	start := time.Now()
	res, err := r.Run(context.Background(), Cmd{Path: "sh", Args: []string{"-c", "sleep 20 & echo started"}, Timeout: 10 * time.Second})
	if err != nil || res.ExitCode != 0 || !strings.Contains(res.Stdout, "started") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Run waited %s for the background child", d)
	}
}
