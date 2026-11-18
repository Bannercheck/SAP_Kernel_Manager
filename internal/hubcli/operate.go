package hubcli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// operations in menu order: key, title, what it does.
var operations = [][3]string{
	{"update", "Kernel Update", "preflight · push archives · backup (SAP up) · stop · extract · verify · start"},
	{"backup", "Backup", "copy every kernel directory next to itself as <name>_<date>"},
	{"files", "Transfer files", "push archives and copy them into the kernel directories"},
	{"stop", "Stop", "StopSystem ALL, wait until every process is GRAY (sapstartsrv keeps running)"},
	{"start", "Start", "sapstartsrv when needed, StartSystem ALL, wait until GREEN"},
	{"services", "Start services", "SAP Host Agent and every sapstartsrv that is down"},
	{"rollback", "Rollback", "copy the latest kernel backup back, verify, start"},
	{"refresh", "Refresh status", "read the hosts of this system again"},
}

var destructive = map[string]bool{"stop": true, "update": true, "rollback": true}

// operate runs one operation on one system with the dashboard's rules:
// unlock, plan preview, the SID typed again, then the job with its progress.
func (c *Console) operate() error {
	sys, ok, err := c.chooseSystem()
	if err != nil || !ok {
		return err
	}
	var names []string
	for _, o := range operations {
		names = append(names, fmt.Sprintf("%-16s %s", o[0], c.pal.Paint(ui.Dim, o[1]+" — "+o[2])))
	}
	i, err := c.pick("Operation on "+sys.SID, names)
	if err != nil || i < 0 {
		return err
	}
	req := hub.JobRequest{Kind: operations[i][0], SID: sys.SID}
	if req.Kind != "refresh" && sys.Unlocked == nil {
		c.printf("\n  %s is read-only in the hub: every change needs an unlock (it lasts %d hours).\n", sys.SID, int(hub.UnlockFor.Hours()))
		if ok, err := c.yes("Unlock " + sys.SID + " now?"); err != nil || !ok {
			return err
		}
		until, err := c.H.Store.SetLock(sys.SID, true)
		if err != nil {
			return err
		}
		c.printf("  %s %s unlocked until %s\n", c.pal.Check(), sys.SID, until.Local().Format("15:04"))
	}
	if req.Kind == "update" || req.Kind == "files" {
		if req.Archives, err = c.chooseArchives(req.Kind, sys); err != nil {
			return err
		}
	}
	if req.HostID, err = c.chooseHost(sys); err != nil {
		return err
	}
	if req.Kind == "update" || req.Kind == "rollback" {
		if req.Start, err = c.yes("Start SAP afterwards (only when every host verified)?"); err != nil {
			return err
		}
	}
	preview := req
	preview.Confirm = req.SID // a preview runs nothing; the real request repeats the SID below
	plan, err := c.H.Plan(preview)
	if err != nil {
		return err
	}
	c.showPlan(plan)
	if destructive[req.Kind] {
		if req.Confirm, err = c.ask("Type "+req.SID+" to confirm (Enter = cancel)", ""); err != nil {
			return err
		}
		if !strings.EqualFold(req.Confirm, req.SID) {
			c.printf("  cancelled: nothing was started\n")
			return nil
		}
	} else if ok, err := c.yes("Run it?"); err != nil || !ok {
		c.printf("  cancelled: nothing was started\n")
		return err
	}
	if c.H.Jobs.Busy() {
		c.printf("  %s another job is running: this one waits in the queue (one job at a time)\n", c.pal.Paint(ui.Yellow, "!"))
	}
	j, err := c.H.Plan(req)
	if err != nil {
		return err
	}
	c.H.Jobs.Submit(j)
	c.follow(j)
	c.reread(sys, req.HostID)
	return nil
}

// reread reads the hosts of a system again after a job, so the next screen
// shows them as they are now.
func (c *Console) reread(sys hub.SystemView, only string) {
	c.printf("  reading the status of %s again …\n", sys.SID)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	for _, h := range sys.Hosts {
		if h.Managed && (only == "" || h.HostID == only) {
			_ = c.H.Refresh(ctx, h.HostID) // an unreachable host shows its error on the screen
		}
	}
}

// chooseArchives lists the library and returns the numbers picked.
func (c *Console) chooseArchives(kind string, sys hub.SystemView) ([]string, error) {
	lib := c.H.Library.List()
	if len(lib) == 0 {
		c.printf("  %s the library is empty: 5) Archives → I imports files from this server\n", c.pal.Paint(ui.Yellow, "!"))
		return nil, nil
	}
	rows := [][]string{c.pal.Headers("#", "ARCHIVE", "COMPONENT", "PATCH", "SIZE")}
	for i, a := range lib {
		comp := a.Label
		if a.Full {
			comp += " (full kernel)"
		}
		if a.Kind == "sapcar" {
			comp = "SAPCAR"
		}
		patch := ""
		if a.Patch > 0 {
			patch = fmt.Sprint(a.Patch)
		}
		rows = append(rows, []string{fmt.Sprint(i + 1), a.Name, comp, patch, size(a.Size)})
	}
	c.printf("\n  %s\n", c.pal.Header("ARCHIVES IN THE HUB LIBRARY"))
	c.table(rows)
	for {
		s, err := c.ask("Archives to push (numbers, e.g. 1,3-4; Enter = none, already on the hosts)", "")
		if err != nil || s == "" {
			return nil, err
		}
		idx, perr := numbers(s, len(lib))
		if perr != nil {
			c.printf("  %s %v\n", c.pal.Paint(ui.Yellow, "!"), perr)
			continue
		}
		var out []string
		top := 0
		for _, i := range idx {
			out = append(out, lib[i].Name)
			top = max(top, lib[i].Patch)
		}
		if kind == "update" {
			c.printf("  Target level after the update: patch %d (now %s)\n", top, kernelShort(sys.Kernel))
		}
		return out, nil
	}
}

// chooseHost returns "" for the whole system or one host's ID.
func (c *Console) chooseHost(sys hub.SystemView) (string, error) {
	var managed []hub.SystemHost
	for _, h := range sys.Hosts {
		if h.Managed {
			managed = append(managed, h)
		}
	}
	if len(managed) < 2 {
		return "", nil
	}
	c.printf("\n")
	for i, h := range managed {
		c.printf("    %s %s %s\n", c.pal.Paint(ui.Bold, fmt.Sprintf("%2d)", i+1)), h.Name, c.pal.Paint(ui.Dim, h.Role))
	}
	for {
		s, err := c.ask(fmt.Sprintf("Hosts: Enter = all %d hosts of %s (central services first), or the number of one host", len(managed), sys.SID), "")
		if err != nil || s == "" {
			return "", err
		}
		if idx, perr := numbers(s, len(managed)); perr == nil && len(idx) == 1 {
			return managed[idx[0]].HostID, nil
		}
		c.printf("  %s one number between 1 and %d\n", c.pal.Paint(ui.Yellow, "!"), len(managed))
	}
}

// showPlan prints the steps grouped like the dashboard: one line per step
// kind with the hosts in the order they run.
func (c *Console) showPlan(j *hub.Job) {
	c.printf("\n  %s — %s\n", c.pal.Header("PLAN"), c.pal.Paint(ui.Bold, j.Title))
	n := 0
	for i := 0; i < len(j.Steps); {
		title := strings.TrimSuffix(j.Steps[i].Title, " on "+j.Steps[i].HostName)
		var hosts []string
		k := i
		for ; k < len(j.Steps) && strings.TrimSuffix(j.Steps[k].Title, " on "+j.Steps[k].HostName) == title; k++ {
			if strings.HasSuffix(j.Steps[k].Title, " on "+j.Steps[k].HostName) {
				hosts = append(hosts, j.Steps[k].HostName)
			}
		}
		n++
		c.printf("  %s %-48s %s\n", c.pal.Paint(ui.Bold, fmt.Sprintf("%2d", n)), title, c.pal.Paint(ui.Dim, strings.Join(hosts, " → ")))
		i = k
	}
	c.printf("  %s\n", c.pal.Paint(ui.Dim, fmt.Sprintf("%d steps in total · one host at a time · the job stops at the first failure", len(j.Steps))))
}

// follow prints the job's console as it runs, with the percentage at every
// step and, on a terminal, a progress line kept at the bottom. Ctrl+C asks
// the hub to stop after the running step (OnInterrupt).
func (c *Console) follow(j *hub.Job) {
	id := j.ID
	c.following.Store(&id)
	defer c.following.Store(nil)
	ch, done := j.Subscribe()
	defer done()
	c.printf("\n  job %s started · Ctrl+C stops after the running step (an extraction or a backup is never cut in half)\n", j.ID)
	seen := 0
	for {
		lines, finished := j.LinesFrom(seen)
		s := j.Summary()
		for _, l := range lines {
			seen = l.N
			c.clearBar()
			c.printLine(l, s.Percent)
		}
		if finished {
			c.clearBar()
			break
		}
		c.bar(s)
		select {
		case <-ch:
		case <-time.After(time.Second):
		}
	}
	s := j.Summary()
	c.printf("\n  %s %s · %d%% · %s · job %s\n", c.jobLight(s.State), c.pal.Paint(ui.Bold, s.Title), s.Percent, s.State, s.ID)
	if s.State != "ok" {
		for _, st := range s.Steps {
			if st.State == "failed" {
				c.printf("  %s %s: %s\n", c.pal.Cross(), st.Title, st.Detail)
			}
		}
	}
}

func (c *Console) printLine(l hub.Line, pct int) {
	switch l.Kind {
	case "step":
		c.printf("\n  %s %s\n", c.pal.Paint(ui.Cyan, fmt.Sprintf("▶ %3d%%", pct)), c.pal.Paint(ui.Bold, l.Text))
	case "ok":
		c.printf("  %s\n", c.pal.Paint(ui.Green, l.Text))
	case "fail":
		c.printf("  %s\n", c.pal.Paint(ui.Red, l.Text))
	default:
		host := l.Host
		if host == "" {
			host = "hub"
		}
		c.printf("  %s %s\n", c.pal.Paint(ui.Dim, fmt.Sprintf("%-10s", host)), l.Text)
	}
}

// bar keeps one progress line at the bottom of a terminal.
func (c *Console) bar(s hub.JobInfo) {
	if !c.TTY {
		return
	}
	const width = 30
	filled := s.Percent * width / 100
	now := ""
	for _, st := range s.Steps {
		if st.State == "running" {
			now = st.Title
			if st.Sub != "" {
				now += " · " + st.Sub
			}
		}
	}
	c.printf("\r\x1b[K  %s%s %s  %s", c.pal.Paint(ui.Green, strings.Repeat("█", filled)), c.pal.Paint(ui.Dim, strings.Repeat("░", width-filled)),
		c.pal.Paint(ui.Bold, fmt.Sprintf("%3d%%", s.Percent)), c.pal.Paint(ui.Dim, now))
}

func (c *Console) clearBar() {
	if c.TTY {
		c.printf("\r\x1b[K")
	}
}

func size(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
}
