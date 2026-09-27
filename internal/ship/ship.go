package ship

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
)

// Options describes one shipment.
type Options struct {
	Hosts     []string
	User      string // remote login, e.g. abcadm
	RemoteDir string // e.g. /usr/sap/download
	Archives  []ops.SARFile
	Program   Program
	SSHOpts   []string // extra ssh/scp options
}

// HostResult is the outcome for one host.
type HostResult struct {
	Host     string
	OK       bool
	Err      error
	Listing  string // ls -la of the remote directory
	Verified int    // files whose checksum matched
	Duration time.Duration
}

// DefaultSSHOpts keep ssh from hanging on host-key questions while still
// recording new keys; passwords, when keys are not set up, are asked by
// ssh itself on the terminal.
var DefaultSSHOpts = []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=15"}

// Send copies the archives and the program to every host, one after the
// other, and verifies the copy with cksum on both sides.
func Send(ctx context.Context, e *ops.Env, o Options) []HostResult {
	if o.RemoteDir == "" {
		o.RemoteDir = "/usr/sap/download"
	}
	if len(o.SSHOpts) == 0 {
		o.SSHOpts = DefaultSSHOpts
	}
	local := localChecksums(ctx, e, o)
	var results []HostResult
	for _, host := range o.Hosts {
		start := time.Now()
		r := HostResult{Host: host}
		r.Err = sendOne(ctx, e, o, host, local, &r)
		r.OK = r.Err == nil
		r.Duration = time.Since(start)
		results = append(results, r)
	}
	return results
}

func (o Options) target(host string) string {
	if o.User != "" {
		return o.User + "@" + host
	}
	return host
}

func (o Options) ssh(host string, args ...string) exec.Cmd {
	return exec.Cmd{Path: "ssh", Args: append(append(append([]string{}, o.SSHOpts...), o.target(host)), args...), Timeout: 30 * time.Minute}
}

func (o Options) scp(args ...string) exec.Cmd {
	return exec.Cmd{Path: "scp", Args: append(append([]string{"-p", "-r"}, o.SSHOpts...), args...), Timeout: 6 * time.Hour}
}

// sendOne runs the numbered steps for one host.
func sendOne(ctx context.Context, e *ops.Env, o Options, host string, local map[string]string, r *HostResult) error {
	const n = 5
	tgt := o.target(host)
	e.Pr.Info(fmt.Sprintf("── %s ──", tgt))
	if err := e.Step(1, n, "Connect to "+host, func() (string, error) {
		out, err := runOK(ctx, e, o.ssh(host, "uname", "-sn"))
		return strings.TrimSpace(out), err
	}); err != nil {
		return err
	}
	if err := e.Step(2, n, "Create "+o.RemoteDir+" on "+host, func() (string, error) {
		_, err := runOK(ctx, e, o.ssh(host, "mkdir", "-p", o.RemoteDir+"/kernelman"))
		return "mkdir -p", err
	}); err != nil {
		return err
	}
	if len(o.Archives) > 0 {
		if err := e.Step(3, n, fmt.Sprintf("Copy %d archive(s) to %s:%s", len(o.Archives), host, o.RemoteDir), func() (string, error) {
			var args []string
			for _, a := range o.Archives {
				args = append(args, a.Path)
			}
			args = append(args, tgt+":"+o.RemoteDir+"/")
			_, err := runOK(ctx, e, o.scp(args...))
			return "scp -p", err
		}); err != nil {
			return err
		}
	} else {
		e.Pr.Info("[3/5] no archives selected: sending KernelMan only")
	}
	if err := e.Step(4, n, "Copy KernelMan to "+host+":"+o.RemoteDir+"/kernelman", func() (string, error) {
		_, err := runOK(ctx, e, o.scp(o.Program.Dir+"/.", tgt+":"+o.RemoteDir+"/kernelman/"))
		if err != nil {
			return "", err
		}
		_, err = runOK(ctx, e, o.ssh(host, "chmod", "-R", "u+x", o.RemoteDir+"/kernelman/kernelman.sh", o.RemoteDir+"/kernelman/bin"))
		return "scp -pr", err
	}); err != nil {
		return err
	}
	return e.Step(5, n, "Verify checksums on "+host, func() (string, error) {
		var remoteFiles []string
		for rel := range local {
			remoteFiles = append(remoteFiles, o.RemoteDir+"/"+rel)
		}
		sort.Strings(remoteFiles)
		out, err := runOK(ctx, e, o.ssh(host, append([]string{"cksum"}, remoteFiles...)...))
		if err != nil {
			return "", err
		}
		remote := parseCksum(out, o.RemoteDir+"/")
		var bad []string
		for rel, sum := range local {
			if remote[rel] != sum {
				bad = append(bad, rel)
			} else {
				r.Verified++
			}
		}
		if len(bad) > 0 {
			sort.Strings(bad)
			return "", fmt.Errorf("checksum mismatch: %s", strings.Join(bad, ", "))
		}
		if ls, err := runOK(ctx, e, o.ssh(host, "ls", "-la", o.RemoteDir)); err == nil {
			r.Listing = ls
		}
		return fmt.Sprintf("%d files match", r.Verified), nil
	})
}

// localChecksums returns rel path (as it will exist under RemoteDir) → "crc size".
func localChecksums(ctx context.Context, e *ops.Env, o Options) map[string]string {
	sums := map[string]string{}
	var paths []string
	rel := map[string]string{}
	for _, a := range o.Archives {
		paths = append(paths, a.Path)
		rel[a.Path] = a.Name
	}
	for _, f := range o.Program.Files {
		p := filepath.Join(o.Program.Dir, f)
		paths = append(paths, p)
		rel[p] = "kernelman/" + filepath.ToSlash(f)
	}
	if len(paths) == 0 {
		return sums
	}
	out, err := runOK(ctx, e, exec.Cmd{Path: "cksum", Args: paths, Timeout: time.Hour})
	if err != nil {
		return sums
	}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		if r, ok := rel[strings.Join(f[2:], " ")]; ok {
			sums[r] = f[0] + " " + f[1]
		}
	}
	return sums
}

// parseCksum maps rel path → "crc size" from cksum output with prefix stripped.
func parseCksum(out, prefix string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		m[strings.TrimPrefix(strings.Join(f[2:], " "), prefix)] = f[0] + " " + f[1]
	}
	return m
}

func runOK(ctx context.Context, e *ops.Env, c exec.Cmd) (string, error) {
	res, err := e.R.Run(ctx, c)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return res.Stdout, fmt.Errorf("%s failed (exit %d): %s", filepath.Base(c.Path), res.ExitCode, lastLine(msg))
	}
	return res.Stdout, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
