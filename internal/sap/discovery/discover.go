package discovery

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
)

// Discover returns the instances on this host, merged from every available
// source, plus non-fatal warnings (a source that was unavailable).
func Discover(ctx context.Context, r exec.Runner, p platform.Platform) ([]Instance, []string) {
	var warnings []string
	var lists [][]Instance

	if list, err := ListInstances(ctx, r, p); err != nil {
		warnings = append(warnings, err.Error())
	} else {
		lists = append(lists, list)
	}

	if path := p.SapservicesPath(); path != "" {
		if b, err := os.ReadFile(path); err != nil {
			warnings = append(warnings, path+": "+err.Error())
		} else {
			lists = append(lists, ParseSapservices(string(b)))
		}
	}

	insts := merge(lists...)
	host, _ := os.Hostname()
	for i := range insts {
		fill(&insts[i].Host, host)
	}
	return insts, warnings
}

// FindSapcontrol locates a sapcontrol executable: PATH first, then the SAP
// Host Agent directory, then any instance executable directory.
func FindSapcontrol(r exec.Runner, p platform.Platform, insts []Instance) (string, error) {
	name := "sapcontrol" + p.ExeSuffix()
	if path, err := r.LookPath("sapcontrol"); err == nil {
		return path, nil
	}
	candidates := []string{filepath.Join(p.HostctrlExeDir(), name)}
	for _, in := range insts {
		if in.ExeDir != "" {
			candidates = append(candidates, filepath.Join(in.ExeDir, name))
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", exec.ErrNotFound
}
