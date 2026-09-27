// Package system resolves the SAP system an operation works on: SID, local
// instances, the <sid>adm user, the central kernel directory (DIR_CT_RUN)
// and a per-SID state directory with a snapshot that survives a stopped
// sapstartsrv.
package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
)

// UsrSap is the SAP root; tests point it at a temporary directory.
var UsrSap = "/usr/sap"

// ErrMultipleSIDs is returned when the host runs several systems and none was chosen.
type ErrMultipleSIDs struct{ SIDs []string }

func (e *ErrMultipleSIDs) Error() string {
	return "several SAP systems on this host, choose one: " + strings.Join(e.SIDs, ", ")
}

// Target is the resolved system context every operation needs.
type Target struct {
	SID        string               `json:"sid"`
	SIDAdm     string               `json:"sidadm"` // abcadm
	Group      string               `json:"group"`  // sapsys
	Instances  []discovery.Instance `json:"instances"`
	KernelDir  string               `json:"kernel_dir"` // DIR_CT_RUN
	DirExeRoot string               `json:"dir_exe_root,omitempty"`
	Sapcontrol string               `json:"sapcontrol,omitempty"`
	StateDir   string               `json:"state_dir"`
	Source     string               `json:"source"` // how KernelDir was found
	Snapshot   *Snapshot            `json:"-"`
}

// Client returns a sapcontrol client for one instance number.
func (t *Target) Client(r exec.Runner, nr string, timeout time.Duration) *sapcontrol.Client {
	return &sapcontrol.Client{Runner: r, Path: t.Sapcontrol, Nr: nr, Timeout: timeout}
}

// Resolve finds the target system. sid may be empty when the host runs one system.
func Resolve(ctx context.Context, r exec.Runner, p platform.Platform, sid string) (*Target, []string, error) {
	insts, warnings := discovery.Discover(ctx, r, p)
	bySID := map[string][]discovery.Instance{}
	for _, in := range insts {
		bySID[in.SID] = append(bySID[in.SID], in)
	}
	if sid == "" {
		if len(bySID) == 0 {
			return nil, warnings, errors.New("no SAP instances found on this host")
		}
		if len(bySID) > 1 {
			var sids []string
			for s := range bySID {
				sids = append(sids, s)
			}
			sort.Strings(sids)
			return nil, warnings, &ErrMultipleSIDs{SIDs: sids}
		}
		for s := range bySID {
			sid = s
		}
	}
	sid = strings.ToUpper(sid)
	t := &Target{SID: sid, SIDAdm: p.SIDAdmUser(sid), Group: "sapsys", Instances: bySID[sid]}
	if len(t.Instances) == 0 {
		return nil, warnings, fmt.Errorf("SAP system %s not found on this host", sid)
	}
	t.StateDir = stateDir(sid)
	t.Snapshot, _ = LoadSnapshot(t.StateDir)
	if scPath, err := discovery.FindSapcontrol(r, p, t.Instances); err == nil {
		t.Sapcontrol = scPath
	} else {
		warnings = append(warnings, "sapcontrol not found on this host")
	}
	if err := t.resolveKernelDir(ctx, r); err != nil {
		return t, warnings, err
	}
	t.SaveSnapshot(func(s *Snapshot) {
		s.KernelDir, s.DirExeRoot, s.Instances, s.TakenAt = t.KernelDir, t.DirExeRoot, t.Instances, time.Now()
	})
	return t, warnings, nil
}

// resolveKernelDir tries sapcontrol, then the snapshot, then SYS/exe/run.
func (t *Target) resolveKernelDir(ctx context.Context, r exec.Runner) error {
	if t.Sapcontrol != "" {
		for _, in := range t.Instances {
			c := t.Client(r, in.Nr, 20*time.Second)
			dir, err := c.ParameterValue(ctx, "DIR_CT_RUN")
			if err != nil || dir == "" {
				continue
			}
			t.KernelDir, t.Source = dir, "sapcontrol ParameterValue DIR_CT_RUN (instance "+in.Nr+")"
			t.DirExeRoot, _ = c.ParameterValue(ctx, "DIR_EXE_ROOT")
			return nil
		}
	}
	if t.Snapshot != nil && t.Snapshot.KernelDir != "" {
		if _, err := os.Stat(t.Snapshot.KernelDir); err == nil {
			t.KernelDir, t.DirExeRoot, t.Source = t.Snapshot.KernelDir, t.Snapshot.DirExeRoot, "snapshot ("+t.StateDir+")"
			return nil
		}
	}
	run := filepath.Join(UsrSap, t.SID, "SYS", "exe", "run")
	if real, err := filepath.EvalSymlinks(run); err == nil {
		t.KernelDir, t.DirExeRoot, t.Source = real, filepath.Join(UsrSap, t.SID, "SYS", "exe"), run+" symlink"
		return nil
	}
	return fmt.Errorf("kernel directory of %s unknown: sapstartsrv not reachable, no snapshot, %s missing", t.SID, run)
}

// stateDir prefers /usr/sap/<SID>/.kernelman and falls back to the home directory.
func stateDir(sid string) string {
	dir := filepath.Join(UsrSap, sid, ".kernelman")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		return dir
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}
	dir = filepath.Join(home, ".kernelman", sid)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
