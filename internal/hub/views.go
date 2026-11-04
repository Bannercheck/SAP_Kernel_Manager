package hub

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
)

// HostState is what the hub knows about a host from its last check.
type HostState struct {
	Checked     time.Time      `json:"checked"`
	OK          bool           `json:"ok"`
	Error       string         `json:"error,omitempty"`
	OS          string         `json:"os,omitempty"`
	Machine     string         `json:"machine,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Report      *status.Report `json:"report,omitempty"`
	Refreshing  bool           `json:"refreshing,omitempty"`
}

// HostView is a host for the dashboard: inventory fields (no secret) plus state.
type HostView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Port      int       `json:"port"`
	User      string    `json:"user"`
	Auth      string    `json:"auth"`
	KeyPath   string    `json:"key_path,omitempty"`
	HasSecret bool      `json:"has_secret"`
	Become    string    `json:"become"`
	SudoPW    bool      `json:"sudo_pw"`
	Label     string    `json:"label,omitempty"`
	SIDs      []string  `json:"sids"`
	Hostname  string    `json:"hostname,omitempty"` // as the host itself reports
	State     HostState `json:"state"`
}

// InstanceView is one SAP instance on a host.
type InstanceView struct {
	Nr          string   `json:"nr"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	Sapstartsrv string   `json:"sapstartsrv"`
	Profile     string   `json:"profile,omitempty"`
	ExeDiffers  []string `json:"exe_differs,omitempty"`
}

// DiskView is the free space of one SAP file system on a host.
type DiskView struct {
	Path    string `json:"path"`
	Mount   string `json:"mount"`
	UsePct  int    `json:"use_pct"`
	AvailKB int64  `json:"avail_kb"`
	SizeKB  int64  `json:"size_kb"`
}

// SystemHost is one host of a system.
type SystemHost struct {
	HostID    string         `json:"host_id,omitempty"` // empty: not in the hub inventory
	Name      string         `json:"name"`
	Hostname  string         `json:"hostname,omitempty"` // as the host reports itself
	OS        string         `json:"os,omitempty"`       // operating system and release
	Label     string         `json:"label,omitempty"`
	Managed   bool           `json:"managed"`
	Reachable bool           `json:"reachable"`
	Central   bool           `json:"central"` // runs (A)SCS
	Role      string         `json:"role"`
	Kernel    string         `json:"kernel,omitempty"`
	Patch     int            `json:"patch,omitempty"`
	KernelDir string         `json:"kernel_dir,omitempty"`
	Instances []InstanceView `json:"instances"`
	Disk      []DiskView     `json:"disk,omitempty"`
	Checked   time.Time      `json:"checked"`
	Error     string         `json:"error,omitempty"`
}

// SystemView is one SAP system across all its hosts.
type SystemView struct {
	SID       string       `json:"sid"`
	Type      string       `json:"type"`
	DB        string       `json:"db"`
	Kernel    string       `json:"kernel"`
	Release   int          `json:"release"`
	Patch     int          `json:"patch"`
	Status    string       `json:"status"` // GREEN YELLOW RED GRAY
	Running   int          `json:"running"`
	Instances int          `json:"instances"`
	Hosts     []SystemHost `json:"hosts"`
	Warnings  []string     `json:"warnings,omitempty"`
	Unlocked  *time.Time   `json:"unlocked_until,omitempty"` // changes allowed until; nil: read-only
}

// buildSystems groups the host reports by SID.
func buildSystems(hosts []Host, states map[string]HostState) []SystemView {
	bySID := map[string]*SystemView{}
	known := map[string]bool{} // short names of every managed host, as configured and as reported
	for _, h := range hosts {
		known[short(h.Name)] = true
		if st := states[h.ID]; st.Report != nil {
			known[short(st.Report.Host.Hostname)] = true
		}
	}
	type remoteKey struct{ sid, host, nr string }
	remotes := map[remoteKey]status.Instance{}
	for _, h := range hosts {
		st := states[h.ID]
		if st.Report == nil {
			continue
		}
		for _, sys := range st.Report.Systems {
			v := bySID[sys.SID]
			if v == nil {
				v = &SystemView{SID: sys.SID}
				bySID[sys.SID] = v
			}
			if v.Type == "" || v.Type == "unknown" {
				v.Type = sys.Type
			}
			if v.DB == "" {
				v.DB = sys.Database.DisplayName()
			}
			sh := SystemHost{HostID: h.ID, Name: h.Name, Label: h.Label, Managed: true, Reachable: st.OK, Checked: st.Checked, Error: st.Error,
				KernelDir: sys.DirCtRun, Patch: sys.Kernel.Patch, Hostname: st.Report.Host.Hostname, OS: st.Report.Host.OSVersion}
			if sys.Kernel.Release > 0 {
				sh.Kernel = sys.Kernel.String()
				if v.Release == 0 || sys.Kernel.Patch < v.Patch { // the system is as old as its oldest host
					v.Release, v.Patch, v.Kernel = sys.Kernel.Release, sys.Kernel.Patch, sys.Kernel.String()
				}
			}
			for _, in := range sys.Instances {
				if !in.Local {
					remotes[remoteKey{sys.SID, short(in.Host), in.Nr}] = in
					continue
				}
				sh.Instances = append(sh.Instances, instanceView(in))
			}
			sh.Central, sh.Role = role(sh.Instances)
			for _, fs := range st.Report.Disk.Filesystems {
				sh.Disk = append(sh.Disk, DiskView{Path: fs.Path, Mount: fs.Mount, UsePct: fs.UsePct, AvailKB: fs.AvailKB, SizeKB: fs.SizeKB})
			}
			v.Hosts = append(v.Hosts, sh)
		}
	}
	// instances on hosts the hub does not manage yet
	for k, in := range remotes {
		if known[k.host] {
			continue
		}
		v := bySID[k.sid]
		var sh *SystemHost
		for i := range v.Hosts {
			if !v.Hosts[i].Managed && v.Hosts[i].Name == in.Host {
				sh = &v.Hosts[i]
			}
		}
		if sh == nil {
			v.Hosts = append(v.Hosts, SystemHost{Name: in.Host})
			sh = &v.Hosts[len(v.Hosts)-1]
		}
		sh.Instances = append(sh.Instances, instanceView(in))
		sh.Central, sh.Role = role(sh.Instances)
	}
	var out []SystemView
	for _, v := range bySID {
		finishSystem(v)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SID < out[j].SID })
	return out
}

func instanceView(in status.Instance) InstanceView {
	name := in.Name
	if name == "" {
		name = in.Type + in.Nr
	}
	return InstanceView{Nr: in.Nr, Name: name, Type: in.Type, Status: in.Status, Sapstartsrv: in.Sapstartsrv, Profile: in.Profile, ExeDiffers: in.ExeDiffers}
}

// role names what a host does from its instance types.
func role(insts []InstanceView) (central bool, r string) {
	var parts []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	for _, in := range insts {
		switch in.Type {
		case "ASCS", "SCS":
			central = true
			add("Central Services")
		case "ERS":
			add("Enqueue Replication")
		case "DVEBMGS":
			central = true
			add("Primary App Server")
		case "D", "J", "W":
			add("Application Server")
		case "HDB":
			add("Database")
		}
	}
	if len(parts) == 0 {
		return central, "—"
	}
	return central, strings.Join(parts, " + ")
}

// finishSystem orders the hosts (central first), aggregates and collects warnings.
func finishSystem(v *SystemView) {
	sort.SliceStable(v.Hosts, func(i, j int) bool {
		a, b := v.Hosts[i], v.Hosts[j]
		if a.Central != b.Central {
			return a.Central
		}
		if a.Managed != b.Managed {
			return a.Managed
		}
		return a.Name < b.Name
	})
	var statuses []string
	levels := map[string][]string{}
	for _, h := range v.Hosts {
		if !h.Managed {
			v.Warnings = append(v.Warnings, fmt.Sprintf("%s is not managed by the hub — add it under Hosts to update its kernel", h.Name))
		} else if !h.Reachable {
			v.Warnings = append(v.Warnings, fmt.Sprintf("%s is not reachable: %s", h.Name, h.Error))
		}
		if h.Kernel != "" {
			levels[h.Kernel] = append(levels[h.Kernel], h.Name)
		}
		for _, in := range h.Instances {
			v.Instances++
			statuses = append(statuses, in.Status)
			if in.Status == "GREEN" {
				v.Running++
			}
			if h.Managed && in.Sapstartsrv == "not running" {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s %s: sapstartsrv is not running", h.Name, in.Name))
			}
			if len(in.ExeDiffers) > 0 {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s %s: local exe is not at the central kernel level (%s differ)", h.Name, in.Name, strings.Join(in.ExeDiffers, ", ")))
			}
		}
		for _, d := range h.Disk {
			if d.UsePct >= 90 {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s %s is %d%% full", h.Name, d.Path, d.UsePct))
			}
		}
	}
	if len(levels) > 1 {
		var parts []string
		for k, hs := range levels {
			parts = append(parts, k+" on "+strings.Join(hs, ", "))
		}
		sort.Strings(parts)
		v.Warnings = append(v.Warnings, "kernel levels differ between hosts: "+strings.Join(parts, " · "))
	}
	v.Status = sapcontrol.Aggregate(statuses)
	if v.Type == "" {
		v.Type = "unknown"
	}
}

func short(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.IndexByte(h, '.'); i > 0 && !isIP(h) {
		return h[:i]
	}
	return h
}

func isIP(h string) bool {
	return strings.Trim(h, "0123456789.") == ""
}
