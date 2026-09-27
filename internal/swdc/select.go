package swdc

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Want describes the kernel to look for.
type Want struct {
	Release      int    // 793
	PlatformDir  string // linuxx86_64, linuxppc64le, rs6000_64
	DB           string // hdb, ora, db6, mss, syb, ada
	CurrentPatch int    // patch level installed now
}

// Pick is a chosen archive with its parsed identity.
type Pick struct {
	Result
	Component string
	Patch     int
	Full      bool
}

// Selection is the proposed download list in apply order.
type Selection struct {
	Picks       []Pick
	StackLevel  int      // level of the chosen full archives
	TargetLevel int      // highest patch after applying everything
	Notes       []string // why things were included or left out
}

var nameRe = regexp.MustCompile(`^(?i)([A-Za-z][A-Za-z0-9_+]*?)[._-](\d{1,4})[._-](\d+)\.sar$`)

var platformWords = map[string][]string{
	"linuxx86_64":  {"LINUX ON X86_64", "LINUX X86_64", "LINUXX86_64", "X86_64 64BIT", "LINUX-X86_64"},
	"linuxppc64le": {"POWER LE", "PPC64LE", "LINUXPPC64LE", "POWERLE"},
	"linuxppc64":   {"LINUX ON POWER 64", "LINUXPPC64 ", "POWER BE"},
	"linuxs390x":   {"Z SYSTEMS", "S390X", "IBM Z"},
	"rs6000_64":    {"AIX", "RS6000"},
	"sunx86_64":    {"SOLARIS ON X86", "SUNX86_64"},
	"sun_64":       {"SOLARIS ON SPARC", "SUN_64"},
	"hpia64":       {"HP-UX", "HPIA64"},
}

var dbWords = map[string][]string{
	"hdb": {"HANA", "HDB"}, "ora": {"ORACLE", "ORA"}, "db6": {"DB2", "DB6"}, "mss": {"MS SQL", "MSSQL", "SQL SERVER"},
	"syb": {"SYBASE", "ASE", "SYB"}, "ada": {"MAXDB", "ADA"},
}

// SearchQueries returns the catalogue queries worth running for a kernel.
func SearchQueries(w Want) []string {
	rel := fmt.Sprintf("%d", w.Release)
	return []string{"SAPEXE_ " + rel, "SAPEXEDB_ " + rel, "dw_ " + rel, "igsexe", "igshelper", "lib_dbsl_ " + rel, "SAPEXE", "SAPEXEDB"}
}

// Choose filters the catalogue results down to what this system needs:
// the highest full SAPEXE/SAPEXEDB archives for the release, platform and
// database, plus every single-component patch above that level, in the
// order Kernel Update applies them.
func Choose(w Want, results []Result) Selection {
	sel := Selection{}
	rel := fmt.Sprintf("%d", w.Release)
	relDot := fmt.Sprintf("%d.%02d", w.Release/100, w.Release%100)
	seen := map[string]bool{}
	var full []Pick
	var comps []Pick
	otherPlatform, otherRelease := 0, 0
	for _, r := range results {
		m := nameRe.FindStringSubmatch(r.Title)
		if m == nil || seen[strings.ToLower(r.Title)] {
			continue
		}
		seen[strings.ToLower(r.Title)] = true
		patch, _ := strconv.Atoi(m[2])
		comp := strings.ToUpper(m[1])
		text := strings.ToUpper(r.Description + " " + r.Info)
		if !(strings.Contains(text, relDot) || strings.Contains(text, " "+rel+" ") || strings.Contains(text, "KERNEL "+rel)) {
			otherRelease++
			continue
		}
		if !matchesPlatform(text, w.PlatformDir) {
			otherPlatform++
			continue
		}
		p := Pick{Result: r, Component: comp, Patch: patch, Full: comp == "SAPEXE" || comp == "SAPEXEDB"}
		if comp == "SAPEXEDB" && w.DB != "" && !containsAny(text, dbWords[w.DB]) {
			sel.Notes = append(sel.Notes, "skipped "+r.Title+" (other database)")
			continue
		}
		if p.Full {
			full = append(full, p)
		} else {
			comps = append(comps, p)
		}
	}
	if otherRelease > 0 {
		sel.Notes = append(sel.Notes, fmt.Sprintf("%d archive(s) of other kernel releases ignored", otherRelease))
	}
	if otherPlatform > 0 {
		sel.Notes = append(sel.Notes, fmt.Sprintf("%d archive(s) for other platforms ignored", otherPlatform))
	}
	// highest SAPEXE and the SAPEXEDB at the same level (or the highest available)
	best := map[string]Pick{}
	for _, p := range full {
		if cur, ok := best[p.Component]; !ok || p.Patch > cur.Patch {
			best[p.Component] = p
		}
	}
	if exe, ok := best["SAPEXE"]; ok {
		sel.StackLevel = exe.Patch
		sel.Picks = append(sel.Picks, exe)
		if db, ok := best["SAPEXEDB"]; ok {
			for _, p := range full { // prefer the SAPEXEDB matching the SAPEXE level
				if p.Component == "SAPEXEDB" && p.Patch == exe.Patch {
					db = p
				}
			}
			sel.Picks = append(sel.Picks, db)
		} else {
			sel.Notes = append(sel.Notes, "no SAPEXEDB archive found for this database: check the database-dependent part by hand")
		}
	} else {
		sel.Notes = append(sel.Notes, "no full SAPEXE archive found for this release and platform")
	}
	// component patches above the stack level, ascending, latest per component/level
	sort.SliceStable(comps, func(i, j int) bool {
		if comps[i].Patch != comps[j].Patch {
			return comps[i].Patch < comps[j].Patch
		}
		return comps[i].Title < comps[j].Title
	})
	for _, p := range comps {
		if p.Patch > sel.StackLevel {
			sel.Picks = append(sel.Picks, p)
		}
	}
	for _, p := range sel.Picks {
		if p.Patch > sel.TargetLevel {
			sel.TargetLevel = p.Patch
		}
	}
	if sel.TargetLevel > 0 && sel.TargetLevel <= w.CurrentPatch {
		sel.Notes = append(sel.Notes, fmt.Sprintf("the system is already at patch %d; nothing newer in the catalogue", w.CurrentPatch))
	}
	return sel
}

func matchesPlatform(text, dir string) bool {
	words, ok := platformWords[dir]
	if !ok {
		return true
	}
	hasAny := false
	for _, ws := range platformWords {
		if containsAny(text, ws) {
			hasAny = true
			break
		}
	}
	if !hasAny { // the entry carries no platform information: cannot exclude it
		return true
	}
	return containsAny(text, words)
}

func containsAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, strings.ToUpper(w)) {
			return true
		}
	}
	return false
}
