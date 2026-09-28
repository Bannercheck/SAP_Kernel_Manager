package ops

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
)

// RemoteInstance is an instance of the target system that runs on another
// host (a distributed system: ASCS here, application servers elsewhere).
type RemoteInstance struct {
	Host     string
	Nr       string
	Status   string // GREEN, YELLOW, RED, GRAY
	Features []string
}

// Label is "app2/02".
func (r RemoteInstance) Label() string { return r.Host + "/" + r.Nr }

// RemoteInstances asks sapcontrol for the system's instance list and returns
// the instances whose number is not among the local ones. Nothing is
// returned when no local sapstartsrv answers: the caller then behaves as on
// a single host.
func RemoteInstances(ctx context.Context, e *Env) []RemoteInstance {
	local := map[string]bool{}
	for _, in := range e.T.Instances {
		local[in.Nr] = true
	}
	for _, in := range e.T.Instances {
		list, err := e.T.Client(e.R, in.Nr, 30*time.Second).GetSystemInstanceList(ctx)
		if err != nil {
			continue
		}
		var out []RemoteInstance
		for _, si := range list {
			if !local[si.Nr] {
				out = append(out, RemoteInstance{Host: si.Hostname, Nr: si.Nr, Status: si.DispStatus, Features: si.Features})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Label() < out[j].Label() })
		return out
	}
	return nil
}

// KernelDirs returns the directories an operation touches: every kernel
// directory on this host, or only the central one (DIR_CT_RUN) when
// CentralOnly is set — the shared /sapmnt copy every host's sapcpe reads.
func (e *Env) KernelDirs() []string { return e.kernelDirs() }

// ProfileDir is the system's profile directory: the directory of a known
// instance profile, else /usr/sap/<SID>/SYS/profile.
func (e *Env) ProfileDir() string {
	for _, in := range e.T.Instances {
		if in.Profile != "" {
			return filepath.Dir(in.Profile)
		}
	}
	return filepath.Join(system.UsrSap, e.T.SID, "SYS", "profile")
}

// WithoutSapcpe names the remote instances whose profiles in profileDir
// (<SID>_<INST>_<host> and START_<INST>_<host>) never mention sapcpe: their
// local exe directory would not pick up a central kernel at start.
func WithoutSapcpe(profileDir string, remote []RemoteInstance) []string {
	var out []string
	for _, r := range remote {
		matches, _ := filepath.Glob(filepath.Join(profileDir, "*_*_"+r.Host))
		found, has := false, false
		for _, m := range matches {
			base := filepath.Base(m) // ABC_D05_app3 or START_D05_app3
			if !strings.HasSuffix(base, r.Nr+"_"+r.Host) {
				continue
			}
			b, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			found = true
			if strings.Contains(strings.ToLower(string(b)), "sapcpe") {
				has = true
			}
		}
		if found && !has {
			out = append(out, r.Label())
		}
	}
	return out
}
