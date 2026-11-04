package hub

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// minFreeKB is the free space a kernel backup and extraction need on
// /usr/sap and /sapmnt (one kernel is about 1.3 GB).
const minFreeKB = 2 << 20

// preflight reads the host's status afresh and refuses to go on when the
// system is not there, sapcontrol cannot see it or space is short. Nothing
// on the host has been changed when a preflight fails.
func (h *Hub) preflight(ctx context.Context, j *Job, st *Step) error {
	if err := h.Refresh(ctx, st.Host); err != nil {
		return err
	}
	s := h.State(st.Host)
	if s.Report == nil {
		return fmt.Errorf("no status from %s", st.HostName)
	}
	found := false
	for _, sys := range s.Report.Systems {
		if !strings.EqualFold(sys.SID, j.SID) {
			continue
		}
		found = true
		local := 0
		for _, in := range sys.Instances {
			if in.Local {
				local++
			}
		}
		if local == 0 {
			return fmt.Errorf("%s has no local instance of %s any more", st.HostName, j.SID)
		}
		if sys.DirCtRun == "" && sys.GlobalKernelDir == "" {
			return fmt.Errorf("the central kernel directory (DIR_CT_RUN) of %s is unknown on %s", j.SID, st.HostName)
		}
		j.log(st.HostName, "out", fmt.Sprintf("  %s on %s: %d local instance(s), kernel %s, central kernel %s", j.SID, st.HostName, local, sys.Kernel.String(), sys.DirCtRun))
	}
	if !found {
		return fmt.Errorf("%s does not report %s", st.HostName, j.SID)
	}
	for _, fs := range s.Report.Disk.Filesystems {
		if fs.AvailKB > 0 && fs.AvailKB < minFreeKB {
			return fmt.Errorf("%s: only %s free on %s, a kernel backup and update need at least %s", st.HostName, humanBytes(fs.AvailKB<<10), fs.Path, humanBytes(minFreeKB<<10))
		}
		j.log(st.HostName, "out", fmt.Sprintf("  %s: %s free (%d%% used)", fs.Path, humanBytes(fs.AvailKB<<10), fs.UsePct))
	}
	return nil
}

// verify reads every host of the job again and requires each local exe
// directory to match the central kernel and, when the archives named one,
// the target patch. A failure stops the job before SAP is started.
func (h *Hub) verify(ctx context.Context, j *Job, st *Step) error {
	want := 0
	if len(st.Args) > 0 {
		want, _ = strconv.Atoi(st.Args[0])
	}
	seen := map[string]bool{}
	var bad, ok []string
	for _, s := range j.Steps {
		if s.Host == "" || seen[s.Host] {
			continue
		}
		seen[s.Host] = true
		if err := h.Refresh(ctx, s.Host); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", s.HostName, err))
			continue
		}
		rep := h.State(s.Host).Report
		for _, sys := range rep.Systems {
			if !strings.EqualFold(sys.SID, j.SID) {
				continue
			}
			if want > 0 && sys.Kernel.Patch != want {
				bad = append(bad, fmt.Sprintf("%s: central kernel is patch %d, expected %d", s.HostName, sys.Kernel.Patch, want))
			}
			for _, in := range sys.Instances {
				switch {
				case !in.Local:
				case len(in.ExeDiffers) > 0:
					bad = append(bad, fmt.Sprintf("%s %s: %s differs from the central kernel (%s)", s.HostName, in.Name, in.DirExecutable, strings.Join(in.ExeDiffers, ", ")))
				default:
					ok = append(ok, s.HostName+" "+in.Name)
				}
			}
			j.log(s.HostName, "out", fmt.Sprintf("  %s on %s: kernel %s", j.SID, s.HostName, sys.Kernel.String()))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("kernel level check failed — SAP is not started: %s", strings.Join(bad, "; "))
	}
	j.log(st.HostName, "ok", "  every instance directory matches the central kernel: "+strings.Join(ok, ", "))
	return nil
}
