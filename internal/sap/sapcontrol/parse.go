package sapcontrol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Process is one row of GetProcessList.
type Process struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DispStatus  string `json:"dispstatus"` // GREEN | YELLOW | RED | GRAY
	TextStatus  string `json:"textstatus"`
	StartTime   string `json:"starttime"`
	ElapsedTime string `json:"elapsedtime"`
	PID         int    `json:"pid"`
}

// SystemInstance is one row of GetSystemInstanceList.
type SystemInstance struct {
	Hostname      string   `json:"hostname"`
	Nr            string   `json:"nr"` // two digits
	HTTPPort      int      `json:"http_port"`
	HTTPSPort     int      `json:"https_port"`
	StartPriority string   `json:"start_priority"`
	Features      []string `json:"features"` // MESSAGESERVER, ENQUE, ABAP, GATEWAY, ICMAN, IGS, J2EE ...
	DispStatus    string   `json:"dispstatus"`
}

// VersionInfo is one row of GetVersionInfo.
type VersionInfo struct {
	Filename   string `json:"filename"`
	Release    int    `json:"release"`
	Patch      int    `json:"patch"`
	Changelist int    `json:"changelist,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Time       string `json:"time,omitempty"`
	Raw        string `json:"raw,omitempty"` // original VersionInfo column
}

// parseTable splits "a, b, c" header + rows. The last column may contain commas.
func parseTable(body string) (header []string, rows [][]string) {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if header == nil {
			header = strings.Split(l, ", ")
			continue
		}
		rows = append(rows, strings.SplitN(l, ", ", len(header)))
	}
	return header, rows
}

func col(row []string, i int) string {
	if i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// ParseProcessList parses the GetProcessList body. The textstatus column may
// itself contain ", " (e.g. "Running, Message Server connection ok, Dialog
// Queue time: 0.00 sec"), so rows are split from both ends: three fixed
// columns on the left, three on the right, textstatus in between.
func ParseProcessList(body string) []Process {
	var out []Process
	first := true
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if first { // header
			first = false
			continue
		}
		f := strings.Split(l, ", ")
		if len(f) < 7 {
			out = append(out, Process{Name: col(f, 0), Description: col(f, 1), DispStatus: col(f, 2), TextStatus: col(f, 3)})
			continue
		}
		n := len(f)
		out = append(out, Process{
			Name: f[0], Description: f[1], DispStatus: f[2],
			TextStatus: strings.Join(f[3:n-3], ", "),
			StartTime:  f[n-3], ElapsedTime: f[n-2], PID: atoi(f[n-1]),
		})
	}
	return out
}

// ParseSystemInstanceList parses the GetSystemInstanceList body.
func ParseSystemInstanceList(body string) []SystemInstance {
	_, rows := parseTable(body)
	out := make([]SystemInstance, 0, len(rows))
	for _, r := range rows {
		si := SystemInstance{
			Hostname: col(r, 0), Nr: fmt.Sprintf("%02d", atoi(col(r, 1))),
			HTTPPort: atoi(col(r, 2)), HTTPSPort: atoi(col(r, 3)), StartPriority: col(r, 4), DispStatus: col(r, 6),
		}
		if f := col(r, 5); f != "" {
			si.Features = strings.Split(f, "|")
		}
		out = append(out, si)
	}
	return out
}

// versionRe matches: <file>, 793, patch 200, changelist 2123456, <anything>, linuxx86_64, 2026 09 14 21:33:27, 0
var versionRe = regexp.MustCompile(
	`^(?P<file>.+?), (?P<rel>\d+), patch (?P<patch>\d+)(?:, changelist (?P<cl>\d+))?(?:, .*?)?, (?P<plat>[A-Za-z0-9_]+), (?P<time>\d{4} \d{2} \d{2} \d{2}:\d{2}:\d{2}), (?P<rks>\d+)$`)

var shortVersionRe = regexp.MustCompile(`(\d{3}), patch (\d+)`)

// ParseVersionInfo parses the GetVersionInfo body.
func ParseVersionInfo(body string) []VersionInfo {
	var out []VersionInfo
	first := true
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if first { // header: Filename, VersionInfo, Time, RKS Compatibility level
			first = false
			continue
		}
		if m := versionRe.FindStringSubmatch(l); m != nil {
			idx := versionRe.SubexpIndex
			out = append(out, VersionInfo{
				Filename: m[idx("file")], Release: atoi(m[idx("rel")]), Patch: atoi(m[idx("patch")]),
				Changelist: atoi(m[idx("cl")]), Platform: m[idx("plat")], Time: m[idx("time")], Raw: l,
			})
			continue
		}
		vi := VersionInfo{Filename: l, Raw: l}
		if i := strings.Index(l, ", "); i > 0 {
			vi.Filename, vi.Raw = l[:i], l[i+2:]
		}
		if m := shortVersionRe.FindStringSubmatch(vi.Raw); m != nil {
			vi.Release, vi.Patch = atoi(m[1]), atoi(m[2])
		}
		out = append(out, vi)
	}
	return out
}

// ParseInstanceProperties parses "property, propertytype, value" rows.
func ParseInstanceProperties(body string) map[string]string {
	_, rows := parseTable(body)
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if k := col(r, 0); k != "" {
			out[k] = col(r, 2)
		}
	}
	return out
}

// Aggregate folds process display statuses into one instance status.
func Aggregate(statuses []string) string {
	if len(statuses) == 0 {
		return "GRAY"
	}
	green, gray := 0, 0
	for _, s := range statuses {
		switch s {
		case "RED":
			return "RED"
		case "YELLOW":
			return "YELLOW"
		case "GREEN":
			green++
		case "GRAY":
			gray++
		}
	}
	switch {
	case green == len(statuses):
		return "GREEN"
	case gray == len(statuses):
		return "GRAY"
	default:
		return "YELLOW"
	}
}
