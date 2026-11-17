package hub

import (
	"testing"
	"time"
)

func TestStepProgress(t *testing.T) {
	st := &Step{Kind: "agent", Args: []string{"update"}, State: "running"}
	for _, l := range []string{"  current kernel: 793 Patch 300", "  [1/20] Move aside … ok", "  [10/20] SAPCAR -xvf … ok"} {
		st.subStep(l)
	}
	if st.Pct != 47 || st.Sub != "10/20" {
		t.Errorf("after 10/20: %d %q", st.Pct, st.Sub)
	}
	st.subStep("  [1/3] StopSystem ALL ... ok") // a new numbered phase never goes back
	if st.Pct < 47 {
		t.Errorf("went back to %d", st.Pct)
	}
	push := &Step{Kind: "push", State: "running"}
	push.uploaded(1, 2, 50) // second of two files, half way
	if push.Pct != 71 || push.Sub != "file 2/2" {
		t.Errorf("push: %d %q", push.Pct, push.Sub)
	}
	steps := []*Step{{Kind: "preflight", State: "ok"}, {Kind: "agent", Args: []string{"update"}, State: "running", Pct: 50}, {Kind: "verify", State: "pending"}}
	if p := jobPercent("running", steps); p != 50 { // (100·1 + 50·8) / (100·10)
		t.Errorf("job percent %d", p)
	}
	if p := jobPercent("ok", steps); p != 100 {
		t.Errorf("finished job %d", p)
	}
}

// The demo update climbs to 100 % without ever going back.
func TestUpdateJobPercent(t *testing.T) {
	h, r := demoHub(t)
	r.Delay = 3 * time.Millisecond
	h.Store.SetLock("DEV", true)
	j, err := h.Plan(JobRequest{Kind: "update", SID: "DEV", Confirm: "DEV", Archives: []string{"SAPEXE_423-80008765.SAR", "SAPEXEDB_423-80008766.SAR"}})
	if err != nil {
		t.Fatal(err)
	}
	h.Jobs.Submit(j)
	last, seen := 0, map[int]bool{}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s := j.Summary()
		if s.Percent < last {
			t.Fatalf("percent went back: %d → %d", last, s.Percent)
		}
		last = s.Percent
		seen[s.Percent] = true
		if s.State != "queued" && s.State != "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if s := j.Summary(); s.State != "ok" || s.Percent != 100 || len(seen) < 6 {
		t.Errorf("state %s, %d %%, %d distinct values", s.State, s.Percent, len(seen))
	}
}
