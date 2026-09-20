// Package discovery finds SAP instances on the local host using the SAP Host
// Agent (saphostctrl), /usr/sap/sapservices and sapstartsrv.
package discovery

import (
	"regexp"
	"sort"
)

// Instance is one SAP instance found on this host.
type Instance struct {
	SID     string `json:"sid"`
	Nr      string `json:"nr"`             // "00"
	Name    string `json:"name,omitempty"` // D00, ASCS01, J00 ...
	Type    string `json:"type,omitempty"` // D, DVEBMGS, ASCS, SCS, J, ERS, HDB, W ...
	Host    string `json:"host,omitempty"`
	Profile string `json:"profile,omitempty"` // instance profile path
	ExeDir  string `json:"exe_dir,omitempty"` // instance executable directory
	// Kernel level as reported by saphostctrl (0 when unknown).
	Release    int    `json:"release,omitempty"`
	Patch      int    `json:"patch,omitempty"`
	Changelist int    `json:"changelist,omitempty"`
	Source     string `json:"source,omitempty"`
}

// Key identifies an instance on a host.
func (i Instance) Key() string { return i.SID + "/" + i.Nr }

// TypeDescription returns a human readable instance type.
func (i Instance) TypeDescription() string { return TypeDescription(i.Type) }

var instanceNameRe = regexp.MustCompile(`^([A-Z]+)(\d{2})$`)

// ParseInstanceName splits "ASCS01" into ("ASCS", "01").
func ParseInstanceName(name string) (typ, nr string, ok bool) {
	m := instanceNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

var typeDescriptions = map[string]string{
	"DVEBMGS": "Primary Application Server (ABAP)",
	"D":       "Application Server (ABAP)",
	"ASCS":    "ABAP Central Services",
	"ERS":     "Enqueue Replication Server",
	"J":       "Application Server (Java)",
	"SCS":     "Java Central Services",
	"HDB":     "SAP HANA Database",
	"W":       "Web Dispatcher",
	"G":       "Gateway",
	"SMDA":    "Diagnostics Agent",
	"TRX":     "TREX",
	"CS":      "Central Services",
}

// TypeDescription maps an instance type code to text.
func TypeDescription(typ string) string {
	if d, ok := typeDescriptions[typ]; ok {
		return d
	}
	if typ == "" {
		return "unknown"
	}
	return typ
}

// merge combines instance lists; later lists fill blanks of earlier entries.
func merge(lists ...[]Instance) []Instance {
	byKey := map[string]*Instance{}
	var order []string
	for _, list := range lists {
		for _, in := range list {
			cur, ok := byKey[in.Key()]
			if !ok {
				c := in
				byKey[in.Key()] = &c
				order = append(order, in.Key())
				continue
			}
			fill(&cur.Name, in.Name)
			fill(&cur.Type, in.Type)
			fill(&cur.Host, in.Host)
			fill(&cur.Profile, in.Profile)
			fill(&cur.ExeDir, in.ExeDir)
			if cur.Release == 0 {
				cur.Release, cur.Patch, cur.Changelist = in.Release, in.Patch, in.Changelist
			}
			if in.Source != "" && cur.Source != in.Source {
				cur.Source += "+" + in.Source
			}
		}
	}
	out := make([]Instance, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].SID != out[b].SID {
			return out[a].SID < out[b].SID
		}
		return out[a].Nr < out[b].Nr
	})
	return out
}

func fill(dst *string, v string) {
	if *dst == "" {
		*dst = v
	}
}
