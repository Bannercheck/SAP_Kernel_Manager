package hub

import (
	"fmt"
	"regexp"
	"strconv"
)

// A job's progress in percent. Every step has a weight by what it does (an
// extraction weighs more than a preflight check); a running step counts with
// its own progress, read from the agent's numbered "[i/n]" lines or from the
// upload of the archives. The value never goes back while a job runs.

var subStepRe = regexp.MustCompile(`^\s*\[(\d+)/(\d+)\]`)

// stepWeight is a rough share of a step in the duration of a job.
func stepWeight(st *Step) int {
	switch st.Kind {
	case "preflight", "verify", "refresh":
		return 1
	case "push":
		return 3
	}
	if len(st.Args) > 0 {
		switch st.Args[0] {
		case "update":
			return 8
		case "rollback":
			return 6
		case "backup":
			return 4
		case "stop", "start":
			return 3
		}
	}
	return 2
}

// jobPercent is the share of the job's weighted steps that is done, 0–100.
func jobPercent(state string, steps []*Step) int {
	if state == "ok" {
		return 100
	}
	total, done := 0, 0 // in percent × weight
	for _, st := range steps {
		w := stepWeight(st)
		total += 100 * w
		switch st.State {
		case "ok":
			done += 100 * w
		case "running", "failed":
			done += st.Pct * w
		}
	}
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

// subStep reads "[i/n]" at the start of an agent line and advances the
// step: a numbered phase fills the step up to 95 %, 100 % comes when the
// step ends. A new phase (n changes or i starts over) continues from where
// the last one stopped.
func (st *Step) subStep(line string) bool {
	m := subStepRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	i, _ := strconv.Atoi(m[1])
	n, _ := strconv.Atoi(m[2])
	if n <= 0 || i <= 0 || i > n {
		return false
	}
	if n != st.subN || i < st.subI {
		st.base = st.Pct
	}
	st.subI, st.subN = i, n
	if p := st.base + (95-st.base)*i/n; p > st.Pct {
		st.Pct = p
	}
	st.Sub = fmt.Sprintf("%d/%d", i, n)
	return true
}

// uploaded sets a push step's progress: file k (0-based) of n at pct.
func (st *Step) uploaded(k, n, pct int) {
	if n <= 0 {
		return
	}
	if p := (k*100 + pct) * 95 / (n * 100); p > st.Pct {
		st.Pct = p
	}
	st.Sub = fmt.Sprintf("file %d/%d", min(k+1, n), n)
}
