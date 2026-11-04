package hub

import (
	"net/http"
	"sort"
	"time"
)

// KPI are the numbers at the top of the dashboard.
type KPI struct {
	Systems          int      `json:"systems"`
	SystemsRunning   int      `json:"systems_running"`
	SystemsStopped   int      `json:"systems_stopped"`
	Hosts            int      `json:"hosts"`
	HostsOK          int      `json:"hosts_ok"`
	Instances        int      `json:"instances"`
	InstancesRunning int      `json:"instances_running"`
	Kernels          []string `json:"kernels"`
	Archives         int      `json:"archives"`
	JobsToday        int      `json:"jobs_today"`
	JobsFailedToday  int      `json:"jobs_failed_today"`
}

// Attention is one line of the "needs attention" panel.
type Attention struct {
	Level string `json:"level"` // error | warn
	SID   string `json:"sid,omitempty"`
	Host  string `json:"host,omitempty"`
	Text  string `json:"text"`
}

// Overview is everything the home page shows.
type Overview struct {
	Systems   []SystemView `json:"systems"`
	Hosts     []HostView   `json:"hosts"`
	Jobs      []JobInfo    `json:"jobs"`
	KPI       KPI          `json:"kpi"`
	Attention []Attention  `json:"attention"`
	Busy      bool         `json:"busy"`
}

// BuildOverview collects the home page.
func (h *Hub) BuildOverview() Overview {
	o := Overview{Systems: nonNil(h.Systems()), Hosts: h.hostViews(), Jobs: h.Jobs.List("", 8), Busy: h.Jobs.Busy(), Attention: []Attention{}}
	kernels := map[string]bool{}
	inSystem := map[string]bool{}
	for _, s := range o.Systems {
		o.KPI.Systems++
		switch s.Status {
		case "GREEN":
			o.KPI.SystemsRunning++
		case "GRAY":
			o.KPI.SystemsStopped++
		}
		o.KPI.Instances += s.Instances
		o.KPI.InstancesRunning += s.Running
		if s.Kernel != "" {
			kernels[s.Kernel] = true
		}
		for _, hs := range s.Hosts {
			inSystem[hs.HostID] = true
		}
		for _, w := range s.Warnings {
			o.Attention = append(o.Attention, Attention{Level: "warn", SID: s.SID, Text: w})
		}
		if s.Status == "RED" {
			o.Attention = append(o.Attention, Attention{Level: "error", SID: s.SID, Text: "processes in RED state"})
		}
	}
	for _, hv := range o.Hosts {
		o.KPI.Hosts++
		if hv.State.OK {
			o.KPI.HostsOK++
		}
		if inSystem[hv.ID] {
			continue
		}
		switch {
		case hv.State.Checked.IsZero():
			o.Attention = append(o.Attention, Attention{Level: "warn", Host: hv.Name, Text: hv.Name + " has not been checked yet"})
		case !hv.State.OK:
			o.Attention = append(o.Attention, Attention{Level: "error", Host: hv.Name, Text: hv.Name + ": " + hv.State.Error})
		case len(hv.SIDs) == 0:
			o.Attention = append(o.Attention, Attention{Level: "warn", Host: hv.Name, Text: hv.Name + ": no SAP instance found"})
		}
	}
	for k := range kernels {
		o.KPI.Kernels = append(o.KPI.Kernels, k)
	}
	sort.Strings(o.KPI.Kernels)
	o.KPI.Archives = len(h.Library.List())
	y, m, d := time.Now().Date()
	for _, j := range h.Jobs.List("", 0) {
		jy, jm, jd := j.Created.Date()
		if jy == y && jm == m && jd == d {
			o.KPI.JobsToday++
			if j.State == "failed" {
				o.KPI.JobsFailedToday++
			}
		}
	}
	sort.SliceStable(o.Attention, func(i, j int) bool { return o.Attention[i].Level == "error" && o.Attention[j].Level != "error" })
	return o
}

func (h *Hub) apiOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, h.BuildOverview())
}
