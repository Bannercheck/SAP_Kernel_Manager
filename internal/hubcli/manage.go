package hubcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

var elevations = []struct{ key, label string }{
	{"", "sudo"}, {"dzdo", "dzdo (Centrify)"}, {"dzdo-su", "dzdo su - (only \"dzdo su -\" allowed)"}, {"sudo-su", "sudo su -"},
}

func runAs(h hub.HostView) string {
	if h.Become == "" {
		return "login user"
	}
	how := "sudo"
	for _, e := range elevations {
		if e.key == h.Elevate {
			how = strings.Fields(e.label)[0]
			if strings.HasSuffix(e.key, "-su") {
				how += " su -"
			}
		}
	}
	who := h.Become
	if who == "sidadm" {
		who = "<sid>adm"
	}
	return who + " via " + how
}

// hosts lists the inventory and adds, edits, deletes or tests a host.
func (c *Console) hosts() error {
	for {
		views := c.H.HostViews()
		rows := [][]string{c.pal.Headers("#", "HOST", "LOGIN", "RUN AS", "SIDS", "STATE")}
		for i, h := range views {
			state := c.pal.Light(ui.Dim) + " not checked"
			switch {
			case h.State.OK:
				state = c.pal.Light(ui.Green) + " ok " + h.State.Checked.Local().Format("15:04")
			case h.State.Error != "":
				state = c.pal.Light(ui.Red) + " " + h.State.Error
			}
			rows = append(rows, []string{fmt.Sprint(i + 1), fmt.Sprintf("%s:%d", h.Name, h.Port), h.User, runAs(h), strings.Join(h.SIDs, " "), state})
		}
		c.printf("\n  %s\n", c.pal.Header("HOSTS"))
		c.table(rows)
		s, err := c.ask("[A] Add  [E] Edit <n>  [D] Delete <n>  [T] Test <n>  [M] Main menu", "m")
		if err != nil {
			return err
		}
		verb, arg, _ := strings.Cut(strings.ToLower(s)+" ", " ")
		n, _ := strconv.Atoi(strings.TrimSpace(arg))
		var target *hub.HostView
		if n >= 1 && n <= len(views) {
			target = &views[n-1]
		}
		switch verb {
		case "a":
			err = c.editHost(nil)
		case "e", "d", "t":
			if target == nil {
				c.printf("  %s give the host number, e.g. %s 2\n", c.pal.Paint(ui.Yellow, "!"), strings.ToUpper(verb))
				continue
			}
			switch verb {
			case "e":
				err = c.editHost(target)
			case "d":
				if ok, yerr := c.yes("Delete " + target.Name + " from the hub? (nothing changes on the host)"); yerr != nil || !ok {
					return yerr
				}
				err = c.H.RemoveHost(target.ID)
			case "t":
				c.test(target.ID, target.Name)
			}
		case "m", "":
			return nil
		}
		if err != nil {
			c.printf("  %s %v\n", c.pal.Cross(), err)
		}
	}
}

// editHost asks every field (current values as defaults), saves, and tests the login.
func (c *Console) editHost(old *hub.HostView) error {
	h := hub.Host{Port: 22, Auth: "password"}
	if old != nil {
		h = hub.Host{ID: old.ID, Name: old.Name, Port: old.Port, User: old.User, Auth: old.Auth, KeyPath: old.KeyPath,
			Become: old.Become, SudoPW: old.SudoPW, Elevate: old.Elevate, Label: old.Label}
	}
	var err error
	if h.Name, err = c.ask("Host name or IP", h.Name); err != nil {
		return err
	}
	port, err := c.ask("SSH port", strconv.Itoa(h.Port))
	if err != nil {
		return err
	}
	h.Port, _ = strconv.Atoi(port)
	if h.User, err = c.ask("Login user (devadm, your domain user, root)", h.User); err != nil {
		return err
	}
	auth, err := c.ask("Login with [P]assword or SSH [K]ey", map[string]string{"password": "p", "key": "k"}[h.Auth])
	if err != nil {
		return err
	}
	secret := ""
	if strings.HasPrefix(strings.ToLower(auth), "k") {
		h.Auth = "key"
		if h.KeyPath, err = c.ask("Private key file on this server", h.KeyPath); err != nil {
			return err
		}
		secret, err = c.secret("Key passphrase (Enter = none or unchanged)")
	} else {
		h.Auth = "password"
		prompt := "SSH password"
		if old != nil && old.HasSecret {
			prompt += " (Enter = unchanged)"
		}
		secret, err = c.secret(prompt)
	}
	if err != nil {
		return err
	}
	as, err := c.ask("Run operations as: [L]ogin user · [S] <sid>adm · [R] root (saproot.sh runs too)", map[string]string{"": "l", "sidadm": "s", "root": "r"}[h.Become])
	if err != nil {
		return err
	}
	h.Become = map[string]string{"s": "sidadm", "r": "root"}[strings.ToLower(as)]
	h.Elevate, h.SudoPW = "", false
	if h.Become != "" {
		def := 1
		for i, e := range elevations {
			if old != nil && e.key == old.Elevate {
				def = i + 1
			}
		}
		for i, e := range elevations {
			c.printf("    %d) %s\n", i+1, e.label)
		}
		how, err := c.ask("Become with", strconv.Itoa(def))
		if err != nil {
			return err
		}
		if k, _ := strconv.Atoi(how); k >= 1 && k <= len(elevations) {
			h.Elevate = elevations[k-1].key
		}
		if h.Auth == "password" {
			if h.SudoPW, err = c.yes("Does sudo / dzdo ask for your password? (it goes to its standard input, never on a command line)"); err != nil {
				return err
			}
		}
	}
	if h.Label, err = c.ask("Label (optional)", h.Label); err != nil {
		return err
	}
	saved, err := c.H.SaveHost(h, secret)
	if err != nil {
		return err
	}
	c.printf("  %s saved · testing the login (SSH, agent, status) …\n", c.pal.Check())
	c.test(saved.ID, saved.Name)
	return nil
}

func (c *Console) test(id, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := c.H.Refresh(ctx, id); err != nil {
		c.printf("  %s %s: %v\n", c.pal.Cross(), name, err)
		return
	}
	st := c.H.State(id)
	c.printf("  %s %s: %s %s · host key %s\n", c.pal.Check(), name, st.OS, st.Machine, st.Fingerprint)
}

// archives shows the library and imports files that are on this server.
func (c *Console) archives() error {
	for {
		lib := c.H.Library.List()
		rows := [][]string{c.pal.Headers("#", "ARCHIVE", "COMPONENT", "PATCH", "SIZE", "SHA-256")}
		for i, a := range lib {
			patch := ""
			if a.Patch > 0 {
				patch = fmt.Sprint(a.Patch)
			}
			rows = append(rows, []string{fmt.Sprint(i + 1), a.Name, a.Label, patch, size(a.Size), a.SHA256[:16] + "…"})
		}
		c.printf("\n  %s %s\n", c.pal.Header("ARCHIVES"), c.pal.Paint(ui.Dim, "(pushed to the hosts and checked with cksum during an operation)"))
		c.table(rows)
		s, err := c.ask("[I] Import a file or directory from this server  [D] Delete <n>  [M] Main menu", "m")
		if err != nil {
			return err
		}
		verb, arg, _ := strings.Cut(strings.ToLower(s)+" ", " ")
		switch verb {
		case "i":
			p, err := c.ask("Path (a .SAR / SAPCAR file, or a directory: all of them)", "")
			if err != nil {
				return err
			}
			c.importPath(p)
		case "d":
			n, _ := strconv.Atoi(strings.TrimSpace(arg))
			if n < 1 || n > len(lib) {
				c.printf("  %s give the number, e.g. D 2\n", c.pal.Paint(ui.Yellow, "!"))
				continue
			}
			if err := c.H.Library.Delete(lib[n-1].Name); err != nil {
				c.printf("  %s %v\n", c.pal.Cross(), err)
			}
		case "m", "":
			return nil
		}
	}
}

func (c *Console) importPath(p string) {
	files := []string{p}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		files = nil
		entries, _ := os.ReadDir(p)
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if !e.IsDir() && (strings.HasSuffix(n, ".sar") || strings.HasPrefix(n, "sapcar")) {
				files = append(files, filepath.Join(p, e.Name()))
			}
		}
	}
	for _, f := range files {
		r, err := os.Open(f)
		if err != nil {
			c.printf("  %s %v\n", c.pal.Cross(), err)
			continue
		}
		a, err := c.H.Library.Save(filepath.Base(f), r)
		r.Close()
		if err != nil {
			c.printf("  %s %s: %v\n", c.pal.Cross(), filepath.Base(f), err)
			continue
		}
		c.printf("  %s %s imported · %s · %s\n", c.pal.Check(), a.Name, size(a.Size), a.Label)
	}
}

// jobs lists the history and shows one job's log.
func (c *Console) jobs() error {
	list := c.H.Jobs.List("", 20)
	rows := [][]string{c.pal.Headers("#", "JOB", "STATE", "%", "STARTED")}
	for i, j := range list {
		rows = append(rows, []string{fmt.Sprint(i + 1), j.Title, c.jobLight(j.State) + " " + j.State, fmt.Sprint(j.Percent), j.Created.Local().Format("01-02 15:04")})
	}
	c.printf("\n  %s\n", c.pal.Header("JOBS"))
	c.table(rows)
	s, err := c.ask("Number = show its log, Enter = main menu", "")
	if err != nil || s == "" {
		return err
	}
	n, _ := strconv.Atoi(s)
	if n < 1 || n > len(list) {
		return nil
	}
	j, ok := c.H.Jobs.Get(list[n-1].ID)
	if !ok {
		return nil
	}
	if s := j.Summary(); s.State == "running" || s.State == "queued" {
		c.follow(j)
		return nil
	}
	lines, _ := j.LinesFrom(0)
	for _, l := range lines {
		c.printLine(l, j.Summary().Percent)
	}
	_, err = c.ask("Enter = main menu", "")
	return err
}

// settings changes where pushed archives land on the hosts.
func (c *Console) settings() error {
	st := c.H.Store.Settings()
	dir, err := c.ask("Archive directory on the hosts", st.RemoteDir)
	if err != nil {
		return err
	}
	st.RemoteDir = dir
	if err := c.H.Store.PutSettings(st); err != nil {
		return err
	}
	c.printf("  %s saved\n", c.pal.Check())
	return nil
}
