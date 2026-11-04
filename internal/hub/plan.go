package hub

import (
	"errors"
	"fmt"
	"strings"
)

// JobRequest is what the dashboard asks for.
type JobRequest struct {
	Kind     string   `json:"kind"`               // backup files stop start update rollback services refresh
	SID      string   `json:"sid"`                // the SAP system
	HostID   string   `json:"host_id,omitempty"`  // only this host (empty: every host of the system)
	Archives []string `json:"archives,omitempty"` // library files to push to the hosts first
	Start    bool     `json:"start,omitempty"`    // update / rollback: start the system afterwards
	Confirm  string   `json:"confirm,omitempty"`  // stop, update, rollback of a whole system: must repeat the SID
}

// opNames are the titles of the operations.
var opNames = map[string]string{
	"backup": "Kernel Backup", "files": "Kernel File Transfer", "stop": "SAP Stop", "start": "SAP Start",
	"update": "Kernel Update", "rollback": "Kernel Rollback", "services": "Start SAP services", "refresh": "Refresh status",
}

// destructive operations need the SID typed again when they cover a whole system.
var destructive = map[string]bool{"stop": true, "update": true, "rollback": true}

// Plan turns a request into an ordered job. Hosts run central services
// first: its sapstartsrv drives StopSystem/StartSystem ALL, and the shared
// central kernel directory (/sapmnt) is backed up and extracted there once.
func (h *Hub) Plan(req JobRequest) (*Job, error) {
	name, ok := opNames[req.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", req.Kind)
	}
	sys, ok := h.System(req.SID)
	if !ok {
		return nil, fmt.Errorf("SAP system %q is not known: refresh the hosts first", req.SID)
	}
	var hosts []SystemHost
	for _, sh := range sys.Hosts {
		if !sh.Managed || (req.HostID != "" && sh.HostID != req.HostID) {
			continue
		}
		if !sh.Reachable && req.Kind != "refresh" {
			return nil, fmt.Errorf("%s is not reachable (%s): refresh it or fix its login first", sh.Name, sh.Error)
		}
		hosts = append(hosts, sh)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no managed host of %s matches", req.SID)
	}
	if req.Kind != "refresh" {
		if _, open := h.Store.Unlocked(sys.SID); !open {
			return nil, fmt.Errorf("%s is read-only in the hub: unlock it on the system page first (an unlock lasts %d hours)", sys.SID, int(UnlockFor.Hours()))
		}
	}
	if destructive[req.Kind] && !strings.EqualFold(strings.TrimSpace(req.Confirm), req.SID) {
		return nil, fmt.Errorf("type the SID %s to confirm %s", req.SID, name)
	}
	if req.HostID == "" && (req.Kind == "update" || req.Kind == "rollback") {
		for _, sh := range sys.Hosts {
			if !sh.Managed {
				var names []string
				for _, in := range sh.Instances {
					names = append(names, in.Name)
				}
				return nil, fmt.Errorf("%s runs %s of %s but is not in the hub: add it under Hosts first, otherwise its kernel would stay behind", sh.Name, strings.Join(names, ", "), sys.SID)
			}
		}
	}
	var archives []Archive
	for _, n := range req.Archives {
		a, err := h.Library.Get(n)
		if err != nil {
			return nil, err
		}
		archives = append(archives, a)
	}
	if len(archives) > 0 && req.Kind != "files" && req.Kind != "update" {
		return nil, errors.New("archives can only be pushed for Kernel File Transfer or Kernel Update")
	}
	if req.Kind == "update" && len(archives) > 0 && targetPatch(archives) == 0 {
		return nil, errors.New("the selected files contain no kernel archive")
	}

	j := &Job{JobInfo: JobInfo{ID: newID(6), Kind: req.Kind, SID: req.SID, Host: req.HostID}}
	j.Title = name + " " + req.SID
	if req.HostID != "" {
		j.Title += " on " + hosts[0].Name
	} else if len(hosts) > 1 {
		j.Title += fmt.Sprintf(" (%d hosts)", len(hosts))
	}
	if p := targetPatch(archives); p > 0 {
		j.Title += fmt.Sprintf(" → patch %d", p)
	}
	add := func(sh SystemHost, kind, title string, args ...string) {
		j.Steps = append(j.Steps, &Step{Host: sh.HostID, HostName: sh.Name, Kind: kind, Title: title, Args: args, State: "pending"})
	}
	each := func(kind, verb string, args ...string) {
		for _, sh := range hosts {
			add(sh, kind, verb+" on "+sh.Name, args...)
		}
	}
	from := func(args ...string) []string {
		if len(archives) > 0 {
			return append(args, "--from", "{pushdir}")
		}
		return args
	}
	push := func() {
		var names []string
		for _, a := range archives {
			names = append(names, a.Name)
		}
		for _, sh := range hosts {
			add(sh, "push", fmt.Sprintf("Push %d file(s) and check cksum on %s", len(names), sh.Name))
			j.Steps[len(j.Steps)-1].Archives = names
		}
	}
	sid := []string{"--sid", req.SID}
	switch req.Kind {
	case "refresh":
		each("refresh", "Read status")
	case "backup":
		each("agent", "Back up kernel directories", append([]string{"backup", "--yes"}, sid...)...)
	case "files":
		if len(archives) > 0 {
			push()
		}
		each("agent", "Copy archives into the kernel directories", from(append([]string{"files", "--yes"}, sid...)...)...)
	case "stop":
		each("agent", "Stop SAP", append([]string{"stop"}, sid...)...)
	case "start":
		each("agent", "Start SAP", append([]string{"start"}, sid...)...)
	case "services":
		each("agent", "Start SAP Host Agent and sapstartsrv", append([]string{"services", "--start", "all"}, sid...)...)
	case "update":
		each("preflight", "Preflight checks")
		if len(archives) > 0 {
			push()
		}
		each("agent", "Stop SAP", append([]string{"stop"}, sid...)...)
		each("agent", "Back up, extract, verify the kernel", from(append([]string{"update", "--yes"}, sid...)...)...)
		add(hosts[0], "verify", "Verify the kernel level on every host", fmt.Sprint(targetPatch(archives)))
		if req.Start {
			each("agent", "Start SAP", append([]string{"start"}, sid...)...)
		}
	case "rollback":
		each("preflight", "Preflight checks")
		each("agent", "Stop SAP", append([]string{"stop"}, sid...)...)
		each("agent", "Restore the latest kernel backup", append([]string{"rollback", "--yes"}, sid...)...)
		add(hosts[0], "verify", "Verify the kernel level on every host", "0")
		if req.Start {
			each("agent", "Start SAP", append([]string{"start"}, sid...)...)
		}
	}
	j.targets = map[string]Target{}
	for _, sh := range hosts {
		host, ok := h.Store.Host(sh.HostID)
		if !ok {
			return nil, fmt.Errorf("host %s is no longer in the inventory", sh.Name)
		}
		t, err := h.target(host)
		if err != nil {
			return nil, err
		}
		j.targets[sh.HostID] = t
	}
	j.pushDirs = map[string]string{}
	return j, nil
}
