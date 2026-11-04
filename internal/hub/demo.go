package hub

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
)

// DemoRemote simulates an SAP landscape so the dashboard can be tried
// without SAP: PRD (ASCS host + three application servers), QAS (one host,
// stopped), DEV (AS Java) and one host that cannot be reached.
type DemoRemote struct {
	Delay time.Duration // pause after every console line

	mu      sync.Mutex
	hosts   map[string]*demoHost
	systems map[string]*demoSys
	files   map[string]map[string]demoFile // host → path → file
	seen    map[string]bool
}

type demoHost struct {
	name, osName, machine, release string
	down                           string
	usrsapPct, sapmntPct           int
}

type demoSys struct {
	SID, Type   string
	DB          status.DB
	Central     int // patch in DIR_CT_RUN
	Prev        int // patch before the last update
	BackupDay   string
	Insts       []*demoInst
	HostBackups map[string]string // host → day of its local backup
}

type demoInst struct {
	Host, Name, Type, Nr string
	Patch                int // level of the local exe directory
	Srv, Running         bool
}

type demoFile struct {
	size  int64
	sum   string
	mtime time.Time
}

// demoPassword is the SSH password of every simulated host.
const demoPassword = "demo"

// NewDemoRemote builds the simulated landscape.
func NewDemoRemote() *DemoRemote {
	r := &DemoRemote{Delay: 70 * time.Millisecond, files: map[string]map[string]demoFile{}, seen: map[string]bool{}}
	h := func(name string, usr, mnt int) *demoHost {
		return &demoHost{name: name, osName: "Linux", machine: "x86_64", release: "Red Hat Enterprise Linux 9.4", usrsapPct: usr, sapmntPct: mnt}
	}
	r.hosts = map[string]*demoHost{
		"prdascs": h("prdascs", 38, 62), "prdapp1": h("prdapp1", 51, 62), "prdapp2": h("prdapp2", 47, 62), "prdapp3": h("prdapp3", 44, 62),
		"qassrv": h("qassrv", 58, 71), "devsrv": h("devsrv", 92, 77), "sbxsrv": {name: "sbxsrv", down: "dial tcp 10.20.30.49:22: connect: no route to host"},
	}
	r.hosts["prdapp3"].release = "SUSE Linux Enterprise Server 15 SP6"
	in := func(host, name, typ, nr string, patch int, running bool) *demoInst {
		return &demoInst{Host: host, Name: name, Type: typ, Nr: nr, Patch: patch, Srv: true, Running: running}
	}
	r.systems = map[string]*demoSys{
		"PRD": {SID: "PRD", Type: "AS ABAP", DB: status.DB{Type: "hdb", Name: "HDB", Host: "prdhdb"}, Central: 200, Prev: 112,
			Insts: []*demoInst{in("prdascs", "ASCS01", "ASCS", "01", 200, true), in("prdapp1", "D00", "D", "00", 200, true),
				in("prdapp2", "D10", "D", "10", 200, true), in("prdapp3", "D20", "D", "20", 112, true)}},
		"QAS": {SID: "QAS", Type: "AS ABAP", DB: status.DB{Type: "hdb", Name: "HDB", Host: "qassrv"}, Central: 300, Prev: 200,
			Insts: []*demoInst{in("qassrv", "ASCS10", "ASCS", "10", 300, false), in("qassrv", "D11", "D", "11", 300, false)}},
		"DEV": {SID: "DEV", Type: "AS Java", DB: status.DB{Type: "ora", Name: "DEV", Host: "devsrv"}, Central: 423, Prev: 300,
			Insts: []*demoInst{in("devsrv", "SCS01", "SCS", "01", 423, true), in("devsrv", "J00", "J", "00", 423, true)}},
	}
	for _, s := range r.systems {
		s.HostBackups = map[string]string{}
	}
	return r
}

// DemoHosts is the inventory the demo starts with.
func DemoHosts() []Host {
	var out []Host
	for _, n := range []struct{ name, label string }{{"prdascs", "PRD · Central Services"}, {"prdapp1", "PRD · PAS"}, {"prdapp2", "PRD · AAS"},
		{"prdapp3", "PRD · AAS"}, {"qassrv", "QAS"}, {"devsrv", "DEV · Java"}, {"sbxsrv", "Sandbox"}} {
		out = append(out, Host{Name: n.name, Port: 22, User: "root", Auth: "password", Label: n.label})
	}
	return out
}

// SetupDemo prepares a fresh hub: admin account, inventory, sample archives.
func SetupDemo(h *Hub, user, password string) error {
	h.agents = func(name string) (*agentBinary, error) {
		return &agentBinary{Name: name, Data: []byte("demo"), Sum: "demo"}, nil
	}
	if h.Store.Auth() == nil {
		a, err := NewAuth(user, password)
		if err != nil {
			return err
		}
		if err := h.Store.SetAuth(a, nil); err != nil {
			return err
		}
	}
	key, err := h.Store.Auth().Key(password)
	if err != nil {
		return err
	}
	h.sess.key = key
	if len(h.Store.Hosts()) == 0 {
		for _, host := range DemoHosts() {
			if host.Secret, err = Seal(key, demoPassword); err != nil {
				return err
			}
			if _, err := h.Store.PutHost(host); err != nil {
				return err
			}
		}
	}
	if len(h.Library.List()) == 0 {
		rng := rand.New(rand.NewPCG(1, 2))
		for _, f := range []struct {
			name string
			kb   int
		}{{"SAPEXE_423-80008765.SAR", 2900}, {"SAPEXEDB_423-80008766.SAR", 1700}, {"igsexe_7-80007787.sar", 900}, {"SAPCAR_1324-70006178.EXE", 300}} {
			b := make([]byte, f.kb<<10)
			for i := range b {
				b[i] = byte(rng.Uint32())
			}
			if _, err := h.Library.Save(f.name, strings.NewReader("CAR 2.01"+string(b))); err != nil {
				return err
			}
		}
	}
	h.RefreshAll(context.Background())
	return nil
}

// Dial checks the simulated host and password.
func (r *DemoRemote) Dial(ctx context.Context, t Target) (Conn, error) {
	r.mu.Lock()
	host, ok := r.hosts[t.Host.Name]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("connect %s: dial tcp: lookup %s: no such host", t.Host.Addr(), t.Host.Name)
	}
	if host.down != "" {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
		}
		return nil, fmt.Errorf("connect %s: %s", t.Host.Addr(), host.down)
	}
	if t.Password != demoPassword {
		return nil, fmt.Errorf("login %s@%s refused: wrong password or key (ssh: unable to authenticate)", t.Host.User, t.Host.Addr())
	}
	r.mu.Lock()
	first := !r.seen[host.name]
	r.seen[host.name] = true
	r.mu.Unlock()
	sum := sha256.Sum256([]byte(host.name))
	return &demoConn{r: r, host: host, fp: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), first: first}, nil
}

type demoConn struct {
	r     *DemoRemote
	host  *demoHost
	fp    string
	first bool
}

func (c *demoConn) Fingerprint() string { return c.fp }
func (c *demoConn) NewKey() bool        { return c.first }
func (c *demoConn) Close() error        { return nil }

func (c *demoConn) Upload(ctx context.Context, rd io.Reader, path string, _ os.FileMode) error {
	var ck cksum
	n, err := io.Copy(&ck, rd)
	if err != nil {
		return err
	}
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	if c.r.files[c.host.name] == nil {
		c.r.files[c.host.name] = map[string]demoFile{}
	}
	c.r.files[c.host.name][path] = demoFile{size: n, sum: ck.Sum(), mtime: time.Now()}
	return nil
}

// Run understands the commands the hub sends: probe, push helpers and the agent.
func (c *demoConn) Run(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	argv := splitQuoted(cmd)
	if len(argv) < 4 || argv[0] != "/bin/sh" {
		return 127, errors.New("demo: unexpected command " + cmd)
	}
	script, args := argv[2], argv[4:]
	switch {
	case script == probeScript:
		dir := "/root/.kernelman/agent"
		if len(args) > 0 && args[0] != "" {
			dir = args[0]
		}
		fmt.Fprintf(stdout, "%s\n%s\n/root\n%s\ndemo\n", c.host.osName, c.host.machine, dir)
	case script == agentDirScript:
	case script == pushDirScript:
		fmt.Fprintln(stdout, args[0])
	case strings.HasPrefix(script, `cksum "$1"`):
		c.r.mu.Lock()
		f, ok := c.r.files[c.host.name][args[0]]
		c.r.mu.Unlock()
		if ok {
			fmt.Fprintf(stdout, "%s %s\n", f.sum, args[0])
		} else if !strings.Contains(script, "|| true") {
			fmt.Fprintf(stderr, "cksum: %s: No such file or directory\n", args[0])
			return 1, nil
		}
	case script == `touch "$1"`:
		c.r.mu.Lock()
		if f, ok := c.r.files[c.host.name][args[0]]; ok {
			f.mtime = time.Now()
			c.r.files[c.host.name][args[0]] = f
		}
		c.r.mu.Unlock()
	case script == `exec "$@"`:
		for i, a := range args {
			if a == "cksum" { // the run-as user reads the pushed archives
				c.r.mu.Lock()
				defer c.r.mu.Unlock()
				for _, p := range args[i+1:] {
					f, ok := c.r.files[c.host.name][p]
					if !ok {
						fmt.Fprintf(stderr, "cksum: %s: No such file or directory\n", p)
						return 1, nil
					}
					fmt.Fprintf(stdout, "%s %s\n", f.sum, p)
				}
				return 0, nil
			}
			if strings.HasSuffix(a, "/kernelman") {
				return c.r.agent(ctx, c.host, args[i+1:], stdout), nil
			}
		}
		return 127, errors.New("demo: no agent in " + cmd)
	default:
		return 127, errors.New("demo: unknown script " + script)
	}
	return 0, nil
}

// splitQuoted splits a command line built with exec.ShellQuote.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inQ, have := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inQ:
			if ch == '\'' {
				inQ = false
			} else {
				cur.WriteByte(ch)
			}
		case ch == '\'':
			inQ, have = true, true
		case ch == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			have = true
		case ch == ' ':
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(ch)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}
