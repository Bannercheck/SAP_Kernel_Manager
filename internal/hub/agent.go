package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// Agent binaries (kernelman-<os>-<arch>) are embedded by `make hub`; a plain
// `go build` embeds only the README and the hub then looks next to its own
// executable, in agents/.
//
//go:embed agents
var bundled embed.FS

// agentBinary is one kernelman build the hub can install.
type agentBinary struct {
	Name string
	Data []byte
	Sum  string // first 16 bytes of the SHA-256, hex
}

var agentCache sync.Map // name → *agentBinary

// agentName maps `uname -s` / `uname -m` to the binary built for that host.
func agentName(unameS, unameM string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(unameS)) {
	case "linux":
		switch strings.TrimSpace(unameM) {
		case "x86_64", "amd64":
			return "kernelman-linux-amd64", nil
		case "ppc64le":
			return "kernelman-linux-ppc64le", nil
		}
		return "", fmt.Errorf("unsupported Linux machine %q (SAP kernels: x86_64, ppc64le)", unameM)
	case "aix":
		return "kernelman-aix-ppc64", nil
	}
	return "", fmt.Errorf("unsupported operating system %q (Linux and AIX are supported)", unameS)
}

// loadAgent returns the embedded binary, or agents/<name> next to the hub executable.
func loadAgent(name string) (*agentBinary, error) {
	if v, ok := agentCache.Load(name); ok {
		return v.(*agentBinary), nil
	}
	data, err := bundled.ReadFile("agents/" + name)
	if err != nil {
		exe, _ := os.Executable()
		for _, dir := range []string{filepath.Join(filepath.Dir(exe), "agents"), filepath.Join(filepath.Dir(exe), "..", "dist", "bin")} {
			if data, err = os.ReadFile(filepath.Join(dir, name)); err == nil {
				break
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("agent %s is not bundled with this hub (build it with `make hub`)", name)
	}
	sum := sha256.Sum256(data)
	a := &agentBinary{Name: name, Data: data, Sum: hex.EncodeToString(sum[:16])}
	agentCache.Store(name, a)
	return a, nil
}

// agentLookup finds the agent build for a host; the demo replaces it.
type agentLookup func(name string) (*agentBinary, error)

// remoteInfo is what the hub learns about a host on every connection.
type remoteInfo struct {
	OS, Machine, Home string
	Agent             string // path of the installed agent
	Installed         bool   // the agent was (re)installed on this connection
}

// probeScript prints uname -s, uname -m, $HOME, the agent directory and the
// installed build. $1 is the agent directory, empty for ~/.kernelman/agent.
const probeScript = `d="${1:-$HOME/.kernelman/agent}"; uname -s; uname -m; echo "$HOME"; echo "$d"; cat "$d/BUILD" 2>/dev/null || echo none`

// agentDirScript prepares a shared agent directory that <sid>adm or root
// (through sudo) can read but nobody else can change.
const agentDirScript = `mkdir -p "$1" && chmod 755 "$1" && test -O "$1" || { echo "$1 is not owned by $(id -un): remove it first" >&2; exit 1; }`

// agentDir is where the agent lives on a host: the login user's home, or —
// when operations run as another user through sudo, who cannot read that
// home — a directory of its own under /var/tmp.
func agentDir(h Host) string {
	if h.Become == "" {
		return ""
	}
	return "/var/tmp/kernelman-" + strings.NewReplacer("@", "_", ".", "_").Replace(h.User)
}

// ensureAgent installs or refreshes the kernelman agent.
func ensureAgent(ctx context.Context, c Conn, h Host, lookup agentLookup, info func(string)) (remoteInfo, error) {
	out, err := output(ctx, c, shCmd(probeScript, agentDir(h)))
	if err != nil {
		return remoteInfo{}, fmt.Errorf("probe host: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 5 {
		return remoteInfo{}, fmt.Errorf("probe host: unexpected answer %q", out)
	}
	ri := remoteInfo{OS: strings.TrimSpace(lines[0]), Machine: strings.TrimSpace(lines[1]), Home: strings.TrimSpace(lines[2])}
	dir := strings.TrimSpace(lines[3])
	if !strings.HasPrefix(dir, "/") {
		return ri, fmt.Errorf("probe host: no usable agent directory (%q)", dir)
	}
	ri.Agent = dir + "/kernelman"
	name, err := agentName(ri.OS, ri.Machine)
	if err != nil {
		return ri, err
	}
	a, err := lookup(name)
	if err != nil {
		return ri, err
	}
	if strings.TrimSpace(lines[4]) == a.Sum {
		return ri, nil
	}
	info(fmt.Sprintf("installing the KernelMan agent (%s, %d MB) in %s", name, len(a.Data)>>20, dir))
	if agentDir(h) != "" {
		if _, err := output(ctx, c, shCmd(agentDirScript, dir)); err != nil {
			return ri, fmt.Errorf("agent directory: %w", err)
		}
	}
	if err := c.Upload(ctx, bytes.NewReader(a.Data), ri.Agent, 0o755); err != nil {
		return ri, err
	}
	if err := c.Upload(ctx, strings.NewReader(a.Sum+"\n"), path.Join(dir, "BUILD"), 0o644); err != nil {
		return ri, err
	}
	if _, err := output(ctx, c, execCmd(ri.Agent, "version")); err != nil {
		return ri, fmt.Errorf("the agent does not run from %s (noexec mount?): %w", dir, err)
	}
	ri.Installed = true
	return ri, nil
}

// SIDAdm is <sid>adm for a SID.
func SIDAdm(sid string) string { return strings.ToLower(sid) + "adm" }

// agentCmd runs the agent with args as the host's run-as user: colours off,
// glyphs on. sid names <sid>adm when the host runs operations as <sid>adm;
// status reads (sid empty) run as the login user then. When sudo asks for a
// password the login password is written to its stdin (sudo -S), never put
// on a command line.
func agentCmd(t Target, ri remoteInfo, sid string, args ...string) (string, io.Reader) {
	argv := []string{"env", "KERNELMAN_COLOR=never", "KERNELMAN_UNICODE=1", ri.Agent}
	var sudo []string
	switch {
	case t.Host.Become == "root":
		sudo = []string{"sudo"}
	case t.Host.Become == "sidadm" && sid != "":
		sudo = []string{"sudo", "-H", "-u", SIDAdm(sid)}
	}
	var stdin io.Reader
	if sudo != nil {
		if t.Host.SudoPW && t.Password != "" {
			sudo = append(sudo[:1], append([]string{"-S", "-p", ""}, sudo[1:]...)...)
			stdin = strings.NewReader(t.Password + "\n")
		} else {
			sudo = append(sudo[:1], append([]string{"-n"}, sudo[1:]...)...)
		}
		argv = append(sudo, argv...)
	}
	return execCmd(append(argv, args...)...), stdin
}
