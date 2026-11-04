package hub

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSSHEndToEnd runs against a real SSH server; it is skipped unless
// KM_E2E_SSH=host:port is set, with KM_E2E_SIDADM / KM_E2E_SIDADM_PW (a
// <sid>adm-like user, ideally with csh) and KM_E2E_USER / KM_E2E_USER_PW (a
// user that may `sudo -u <sid>adm` with its password). make hub builds the
// embedded agents first.
func TestSSHEndToEnd(t *testing.T) {
	addr := os.Getenv("KM_E2E_SSH")
	if addr == "" {
		t.Skip("set KM_E2E_SSH=host:port to run against a real SSH server")
	}
	host, port, _ := strings.Cut(addr, ":")
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &SSHRemote{Store: st}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sidadm := os.Getenv("KM_E2E_SIDADM")
	mk := func(user string) Host {
		h := Host{Name: host, User: user, Auth: "password"}
		if port != "" {
			h.Port = atoiDefault(port, 22)
		}
		return h
	}

	// wrong password: refused with a clear message
	if _, err := r.Dial(ctx, Target{Host: mk(sidadm), Password: "definitely-wrong"}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("wrong password: %v", err)
	}

	// <sid>adm with its own login shell (csh on SAP hosts): probe, install, run
	c, err := r.Dial(ctx, Target{Host: mk(sidadm), Password: os.Getenv("KM_E2E_SIDADM_PW")})
	if err != nil {
		t.Fatal(err)
	}
	// the key was pinned on the very first contact (the wrong-password attempt), before authentication
	if c.NewKey() || st.HostKey(mk(sidadm).Addr()) == "" || !strings.HasPrefix(c.Fingerprint(), "SHA256:") {
		t.Errorf("host key pinning: new=%v stored=%q fp=%q", c.NewKey(), st.HostKey(mk(sidadm).Addr()), c.Fingerprint())
	}
	ri, err := ensureAgent(ctx, c, mk(sidadm), loadAgent, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	cmd, stdin := agentCmd(Target{Host: mk(sidadm)}, ri, "DEV", "version")
	var out bytes.Buffer
	if code, err := c.Run(ctx, cmd, stdin, &out, &out); err != nil || code != 0 || !strings.Contains(out.String(), "kernelman") {
		t.Errorf("agent as %s: %d %v %q", sidadm, code, err, out.String())
	}
	c.Close()

	// a domain-like user running the agent as <sid>adm through sudo with its password
	user := os.Getenv("KM_E2E_USER")
	h := mk(user)
	h.Become, h.SudoPW = "sidadm", true
	tg := Target{Host: h, Password: os.Getenv("KM_E2E_USER_PW")}
	c, err = r.Dial(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.NewKey() {
		t.Error("the pinned key was trusted again")
	}
	ri, err = ensureAgent(ctx, c, h, loadAgent, func(s string) { t.Log(s) })
	if err != nil || !strings.HasPrefix(ri.Agent, "/var/tmp/kernelman-") {
		t.Fatalf("shared agent dir: %q %v", ri.Agent, err)
	}
	sid := strings.TrimSuffix(sidadm, "adm")
	cmd, stdin = agentCmd(tg, ri, sid, "version")
	out.Reset()
	if code, err := c.Run(ctx, cmd, stdin, &out, &out); err != nil || code != 0 || !strings.Contains(out.String(), "kernelman") {
		t.Errorf("sudo -u %s: %d %v %q", sidadm, code, err, out.String())
	}
	out.Reset()
	if _, err := c.Run(ctx, execCmd("sudo", "-S", "-p", "", "-u", sidadm, "id", "-un"), strings.NewReader(tg.Password+"\n"), &out, &out); err != nil || strings.TrimSpace(out.String()) != sidadm {
		t.Errorf("sudo identity: %q %v", out.String(), err)
	}

	// a changed host key is refused
	st.TrustHostKey(h.Addr(), "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl")
	if _, err := r.Dial(ctx, tg); err == nil || !strings.Contains(err.Error(), "HOST KEY CHANGED") {
		t.Errorf("changed host key: %v", err)
	}
}

func atoiDefault(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}
