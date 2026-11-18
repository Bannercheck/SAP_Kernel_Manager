package hubcli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// home shows every system as one row, what needs attention, the last jobs
// and the menu — the console's overview page.
func (c *Console) home() {
	o := c.H.BuildOverview()
	host, _ := os.Hostname()
	c.printf("\n  %s · %s · %s\n", c.pal.Paint(ui.Bold, "KernelMan Hub"), host, time.Now().Format("2006-01-02 15:04"))
	k := o.KPI
	c.printf("  %d systems (%d running, %d stopped) · %d/%d hosts reachable · %d/%d instances running · kernels: %s\n",
		k.Systems, k.SystemsRunning, k.SystemsStopped, k.HostsOK, k.Hosts, k.InstancesRunning, k.Instances, kernels(k.Kernels))
	if len(o.Systems) == 0 {
		c.printf("\n  No SAP system yet: 4) Hosts → A adds the first host (SSH as root, <sid>adm or your domain user).\n")
	} else {
		rows := [][]string{c.pal.Headers("SYSTEM", "TYPE", "STATE", "KERNEL", "HOSTS", "INSTANCES", "CHANGES")}
		for _, s := range o.Systems {
			rows = append(rows, []string{c.pal.Paint(ui.Bold, s.SID), s.Type, c.state(s.Status), kernelShort(s.Kernel),
				plural(len(s.Hosts), "host"), fmt.Sprintf("%d/%d running", s.Running, s.Instances), c.access(s)})
		}
		c.printf("\n")
		c.table(rows)
	}
	if len(o.Attention) > 0 {
		c.printf("\n  %s\n", c.pal.Header("NEEDS ATTENTION"))
		for _, a := range o.Attention {
			mark := c.pal.Paint(ui.Yellow, "!")
			if a.Level == "error" {
				mark = c.pal.Cross()
			}
			who := a.SID
			if a.Host != "" {
				who = strings.TrimSpace(who + " " + a.Host)
			}
			c.printf("  %s %s %s\n", mark, c.pal.Paint(ui.Bold, who), a.Text)
		}
	}
	if len(o.Jobs) > 0 {
		c.printf("\n  %s\n", c.pal.Header("RECENT JOBS"))
		for i, j := range o.Jobs {
			if i == 3 {
				break
			}
			c.printf("  %s %4d%%  %s  %s\n", c.jobLight(j.State), j.Percent, j.Title, c.pal.Paint(ui.Dim, j.Created.Local().Format("01-02 15:04")))
		}
	}
	c.printf("\n")
	for _, m := range [][3]string{{"1", "Refresh", "read the status of every host again"},
		{"2", "System", "details of one system: hosts, instances, local exe = central, disk"},
		{"3", "Run an operation", "Kernel Update · Backup · Transfer files · Stop · Start · Start services · Rollback"},
		{"4", "Hosts", "list · add · edit · delete · test the login"},
		{"5", "Archives", "the hub's library: import SAR / SAPCAR files from this server, delete"},
		{"6", "Jobs", "history and logs"},
		{"7", "Settings", "archive directory on the hosts"},
		{"q", "Quit", ""}} {
		c.printf("   %s) %s %s\n", m[0], c.pal.Paint(ui.Bold, fmt.Sprintf("%-17s", m[1])), c.pal.Paint(ui.Dim, m[2]))
	}
}

func (c *Console) table(rows [][]string) {
	for _, l := range ui.Table("  ", rows) {
		c.printf("%s\n", l)
	}
}

// state is the light and word of a system or instance status.
func (c *Console) state(s string) string {
	switch s {
	case "GREEN":
		return c.pal.Light(ui.Green) + " running"
	case "YELLOW", "RED":
		return c.pal.Light(ui.Yellow) + " partial"
	case "GRAY":
		return c.pal.Light(ui.Red) + " stopped"
	}
	return c.pal.Light(ui.Dim) + " unknown"
}

func (c *Console) access(s hub.SystemView) string {
	if s.Unlocked != nil {
		return c.pal.Paint(ui.Yellow, "unlocked until "+s.Unlocked.Local().Format("15:04"))
	}
	return c.pal.Paint(ui.Dim, "read-only")
}

func (c *Console) jobLight(state string) string {
	switch state {
	case "ok":
		return c.pal.Check()
	case "failed", "cancelled", "interrupted":
		return c.pal.Cross()
	}
	return c.pal.Light(ui.Cyan)
}

func kernelShort(k string) string { return strings.Replace(k, " Patch ", " PL ", 1) }

func kernels(ks []string) string {
	if len(ks) == 0 {
		return "—"
	}
	var out []string
	for _, k := range ks {
		out = append(out, kernelShort(k))
	}
	return strings.Join(out, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// refreshAll reads every host again (SSH, agent, status).
func (c *Console) refreshAll() {
	c.printf("  reading the status of %s …\n", plural(len(c.H.Store.Hosts()), "host"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c.H.RefreshAll(ctx)
}

// chooseSystem asks for a SID among the known systems.
func (c *Console) chooseSystem() (hub.SystemView, bool, error) {
	systems := c.H.Systems()
	if len(systems) == 0 {
		c.printf("  no SAP system yet: add a host first (4)\n")
		return hub.SystemView{}, false, nil
	}
	var names []string
	for _, s := range systems {
		names = append(names, fmt.Sprintf("%-4s %s · %s · %s", s.SID, s.Type, kernelShort(s.Kernel), plural(len(s.Hosts), "host")))
	}
	i, err := c.pick("System", names)
	if err != nil || i < 0 {
		return hub.SystemView{}, false, err
	}
	return systems[i], true, nil
}

// systemDetails shows one system host by host, like the dashboard's system page.
func (c *Console) systemDetails() error {
	s, ok, err := c.chooseSystem()
	if err != nil || !ok {
		return err
	}
	central := ""
	for _, h := range s.Hosts {
		if h.Central {
			central = " · central services on " + h.Name
		}
	}
	c.printf("\n  %s  %s · %s · %s%s\n", c.pal.Paint(ui.Bold, s.SID), s.Type, s.DB, plural(len(s.Hosts), "host"), central)
	c.printf("  Kernel %s · %d/%d instances running · %s · %s\n", kernelShort(s.Kernel), s.Running, s.Instances, c.state(s.Status), c.access(s))
	for _, w := range s.Warnings {
		c.printf("  %s %s\n", c.pal.Paint(ui.Yellow, "!"), w)
	}
	rows := [][]string{c.pal.Headers("HOST", "OS", "ROLE", "INSTANCES", "LOCAL EXE = CENTRAL", "DISK", "CHECKED")}
	for _, h := range s.Hosts {
		var insts, differ, disk []string
		for _, in := range h.Instances {
			insts = append(insts, c.instLight(in.Status)+" "+in.Name)
			if len(in.ExeDiffers) > 0 {
				differ = append(differ, in.Name+": "+strings.Join(in.ExeDiffers, ", "))
			}
		}
		sync := c.pal.Paint(ui.Green, "in sync")
		if len(differ) > 0 {
			sync = c.pal.Paint(ui.Red, "differs ") + strings.Join(differ, "; ")
		}
		for _, d := range h.Disk {
			disk = append(disk, fmt.Sprintf("%s %d%%", d.Path, d.UsePct))
		}
		name := h.Name
		if !h.Managed {
			name += c.pal.Paint(ui.Red, " (not in the hub)")
		} else if !h.Reachable {
			name += c.pal.Paint(ui.Red, " (unreachable)")
		}
		checked := "—"
		if !h.Checked.IsZero() {
			checked = h.Checked.Local().Format("15:04")
		}
		rows = append(rows, []string{name, h.OS, h.Role, strings.Join(insts, " "), sync, strings.Join(disk, " "), checked})
	}
	c.printf("\n")
	c.table(rows)
	_, err = c.ask("Enter = main menu", "")
	return err
}

func (c *Console) instLight(status string) string {
	switch status {
	case "GREEN":
		return c.pal.Light(ui.Green)
	case "YELLOW", "RED":
		return c.pal.Light(ui.Yellow)
	case "GRAY":
		return c.pal.Light(ui.Red)
	}
	return c.pal.Light(ui.Dim)
}
