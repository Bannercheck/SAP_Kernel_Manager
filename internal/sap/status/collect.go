package status

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
)

// Options narrows and tunes collection.
type Options struct {
	SID     string        // only this system ("" = all)
	Timeout time.Duration // per sapcontrol call
}

// Collect builds the Report. Failures of individual probes are recorded in
// the report instead of aborting, so the screen always shows what is known.
func Collect(ctx context.Context, r exec.Runner, p platform.Platform, opts Options) *Report {
	rep := &Report{GeneratedAt: time.Now()}
	rep.Host = hostInfo(ctx, r, p)
	rep.HostAgent = discovery.CheckHostAgent(ctx, r, p)

	insts, warnings := discovery.Discover(ctx, r, p)
	rep.Warnings = append(rep.Warnings, warnings...)

	sapcontrolPath, err := discovery.FindSapcontrol(r, p, insts)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "sapcontrol not found; instance status unavailable")
	}

	bySID := map[string][]discovery.Instance{}
	for _, in := range insts {
		if opts.SID != "" && !strings.EqualFold(in.SID, opts.SID) {
			continue
		}
		bySID[in.SID] = append(bySID[in.SID], in)
	}
	sids := make([]string, 0, len(bySID))
	for sid := range bySID {
		sids = append(sids, sid)
	}
	sort.Strings(sids)
	for _, sid := range sids {
		rep.Systems = append(rep.Systems, collectSystem(ctx, r, p, sapcontrolPath, sid, bySID[sid], opts))
	}
	if len(insts) == 0 {
		rep.Warnings = append(rep.Warnings, "no SAP instances found on this host")
	}
	return rep
}

func hostInfo(ctx context.Context, r exec.Runner, p platform.Platform) HostInfo {
	h := HostInfo{OS: p.Name(), Arch: p.Arch(), KernelDirName: p.KernelDirName(), Privileged: p.IsPrivileged()}
	h.Hostname, _ = os.Hostname()
	h.OSVersion = p.OSVersion(ctx, r)
	if u, err := user.Current(); err == nil {
		h.User = u.Username
	}
	return h
}

func collectSystem(ctx context.Context, r exec.Runner, p platform.Platform, scPath, sid string,
	local []discovery.Instance, opts Options) System {
	sys := System{SID: sid, Status: "GRAY"}
	var ref *sapcontrol.Client // first instance whose sapstartsrv answers
	var statuses []string

	for _, li := range local {
		in := Instance{Nr: li.Nr, Name: li.Name, Type: li.Type, Host: li.Host, Local: true,
			Profile: li.Profile, DirExecutable: li.ExeDir, Sapstartsrv: "unknown", Status: "GRAY"}
		if scPath != "" {
			c := &sapcontrol.Client{Runner: r, Path: scPath, Nr: li.Nr, Timeout: opts.Timeout}
			probeInstance(ctx, c, &in)
			if in.Sapstartsrv == "running" && ref == nil {
				ref = c
			}
		}
		in.TypeDesc = discovery.TypeDescription(in.Type)
		statuses = append(statuses, in.Status)
		sys.Instances = append(sys.Instances, in)
	}

	if ref != nil {
		collectSystemParams(ctx, ref, &sys)
		if list, err := ref.GetSystemInstanceList(ctx); err == nil {
			addRemoteInstances(&sys, list, &statuses)
		} else {
			sys.Errors = append(sys.Errors, err.Error())
		}
	}
	sys.Kernel, sys.KernelSource = kernelVersion(ctx, r, p, ref, &sys, local)
	sys.Type = systemType(sys.Instances)
	sys.Status = sapcontrol.Aggregate(statuses)
	return sys
}

// probeInstance fills sapstartsrv state, processes, name and local exe dir.
func probeInstance(ctx context.Context, c *sapcontrol.Client, in *Instance) {
	procs, err := c.GetProcessList(ctx)
	switch {
	case err == nil:
		in.Sapstartsrv, in.Processes = "running", procs
		st := make([]string, 0, len(procs))
		for _, pr := range procs {
			st = append(st, pr.DispStatus)
		}
		in.Status = sapcontrol.Aggregate(st)
	case sapcontrol.IsConnRefused(err):
		in.Sapstartsrv = "not running"
		return
	default:
		in.Error = err.Error()
		return
	}
	if in.Name == "" {
		if props, err := c.GetInstanceProperties(ctx); err == nil {
			in.Name = props["INSTANCE_NAME"]
			if typ, _, ok := discovery.ParseInstanceName(in.Name); ok {
				in.Type = typ
			}
		}
	}
	if v, err := c.ParameterValue(ctx, "DIR_EXECUTABLE"); err == nil && v != "" {
		in.DirExecutable = v
	}
	if in.Profile == "" {
		if v, err := c.ParameterValue(ctx, "SAPPROFILE"); err == nil {
			in.Profile = v
		}
	}
}

func collectSystemParams(ctx context.Context, c *sapcontrol.Client, sys *System) {
	get := func(name string) string {
		v, err := c.ParameterValue(ctx, name)
		if err != nil {
			sys.Errors = append(sys.Errors, err.Error())
		}
		return v
	}
	sys.DirCtRun = get("DIR_CT_RUN")
	sys.DirExeRoot = get("DIR_EXE_ROOT")
	if sys.DirCtRun != "" {
		if real, err := filepath.EvalSymlinks(sys.DirCtRun); err == nil {
			sys.GlobalKernelDir = real
		}
	}
	sys.Database.Type = strings.ToLower(get("dbms/type"))
	sys.Database.Host = get("SAPDBHOST")
	if sys.Database.Type == "hdb" {
		sys.Database.Name = get("dbs/hdb/dbname")
	}
}

func addRemoteInstances(sys *System, list []sapcontrol.SystemInstance, statuses *[]string) {
	local := map[string]int{}
	for i, in := range sys.Instances {
		local[in.Nr] = i
	}
	host, _ := os.Hostname()
	for _, si := range list {
		if i, ok := local[si.Nr]; ok && isLocalHost(si.Hostname, host, sys.Instances[i].Host) {
			sys.Instances[i].Features = si.Features
			continue
		}
		in := Instance{Nr: si.Nr, Host: si.Hostname, Features: si.Features, Sapstartsrv: "remote", Status: si.DispStatus}
		in.Type = typeFromFeatures(si.Features)
		in.TypeDesc = discovery.TypeDescription(in.Type)
		*statuses = append(*statuses, in.Status)
		sys.Instances = append(sys.Instances, in)
	}
}

// isLocalHost reports whether name refers to this machine or the host the
// instance was discovered on (short and fully qualified names both match).
func isLocalHost(name, thisHost, discoveredHost string) bool {
	n := shortHost(strings.ToLower(name))
	return n == shortHost(strings.ToLower(thisHost)) || n == shortHost(strings.ToLower(discoveredHost))
}

func shortHost(h string) string {
	if i := strings.IndexByte(h, '.'); i > 0 {
		return h[:i]
	}
	return h
}

// kernelVersion prefers disp+work -V in DIR_CT_RUN, then GetVersionInfo,
// then what saphostctrl reported.
func kernelVersion(ctx context.Context, r exec.Runner, p platform.Platform, ref *sapcontrol.Client,
	sys *System, local []discovery.Instance) (kernel.Version, string) {
	if sys.DirCtRun != "" {
		if v, err := kernel.Probe(ctx, r, p, sys.DirCtRun); err == nil {
			return v, "disp+work -V (" + sys.DirCtRun + ")"
		} else {
			sys.Errors = append(sys.Errors, err.Error())
		}
	}
	if ref != nil {
		if rows, err := ref.GetVersionInfo(ctx); err == nil {
			for _, row := range rows {
				if row.Release > 0 {
					return kernel.Version{Release: row.Release, Patch: row.Patch, Changelist: row.Changelist, Platform: row.Platform},
						"sapcontrol GetVersionInfo (" + filepath.Base(row.Filename) + ")"
				}
			}
		}
	}
	for _, li := range local {
		if li.Release > 0 {
			return kernel.Version{Release: li.Release, Patch: li.Patch, Changelist: li.Changelist}, "saphostctrl ListInstances"
		}
	}
	return kernel.Version{}, "unavailable (no running sapstartsrv, no saphostctrl data)"
}

func typeFromFeatures(features []string) string {
	has := func(f string) bool {
		for _, x := range features {
			if x == f {
				return true
			}
		}
		return false
	}
	switch {
	case has("ABAP"):
		return "D"
	case has("J2EE"):
		return "J"
	case has("ENQREP"):
		return "ERS"
	case has("MESSAGESERVER"), has("ENQUE"):
		return "CS"
	case has("HDB"):
		return "HDB"
	case has("WEBDISP"):
		return "W"
	}
	return ""
}

func systemType(insts []Instance) string {
	abap, java, hana := false, false, false
	for _, in := range insts {
		switch in.Type {
		case "D", "DVEBMGS", "ASCS":
			abap = true
		case "J", "SCS":
			java = true
		case "HDB":
			hana = true
		}
		for _, f := range in.Features {
			switch f {
			case "ABAP":
				abap = true
			case "J2EE":
				java = true
			}
		}
	}
	switch {
	case abap && java:
		return "Dual-stack (ABAP+Java)"
	case abap:
		return "ABAP"
	case java:
		return "Java"
	case hana:
		return "HANA database"
	}
	return "unknown"
}
