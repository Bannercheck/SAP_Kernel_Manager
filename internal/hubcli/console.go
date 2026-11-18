// Package hubcli is the console front end of KernelMan Hub: the same
// inventory, plans, safety rules and jobs as the dashboard, on a terminal
// (for example RHEL over ssh) — no browser and no open port.
package hubcli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// Console is one interactive session on the hub.
type Console struct {
	H   *hub.Hub
	in  *bufio.Reader
	out io.Writer
	pal ui.Palette
	// TTY: the input is a terminal; passwords are read without echo and a
	// running job keeps a progress line at the bottom.
	TTY bool

	following atomic.Pointer[string] // ID of the job being shown, if any
}

// New prepares a console on in/out.
func New(h *hub.Hub, in io.Reader, out io.Writer, pal ui.Palette) *Console {
	return &Console{H: h, in: bufio.NewReader(in), out: out, pal: pal}
}

var errQuit = errors.New("quit")

// Run logs in and shows the main menu until the operator quits. The exit
// code is 0 after q (or the end of the input) and 1 when the login fails.
func (c *Console) Run() int {
	c.printf("\n  %s\n", c.pal.Paint(ui.Bold, "KernelMan Hub — console"))
	if err := c.login(); err != nil {
		c.printf("  %s %v\n", c.pal.Cross(), err)
		return 1
	}
	for {
		c.home()
		choice, err := c.ask("Select (number, q to quit)", "")
		if err != nil {
			return 0
		}
		switch strings.ToLower(choice) {
		case "1", "refresh":
			c.refreshAll()
		case "2", "system", "details":
			err = c.systemDetails()
		case "3", "run", "operation":
			err = c.operate()
		case "4", "hosts":
			err = c.hosts()
		case "5", "archives":
			err = c.archives()
		case "6", "jobs":
			err = c.jobs()
		case "7", "settings":
			err = c.settings()
		case "q", "quit", "exit":
			return 0
		}
		if errors.Is(err, io.EOF) || errors.Is(err, errQuit) {
			return 0
		}
		if err != nil {
			c.printf("  %s %v\n", c.pal.Cross(), err)
		}
	}
}

// login creates the admin account on first start, otherwise checks it
// (three attempts). The account is the dashboard's: one inventory, one vault.
func (c *Console) login() error {
	if c.H.Store.Auth() == nil {
		c.printf("  First start: create the hub administrator (the dashboard uses the same account).\n")
		c.printf("  Its password also encrypts the stored SSH passwords: at least %d characters.\n", hub.MinPasswordLen)
		for {
			user, err := c.ask("Administrator user", "admin")
			if err != nil {
				return err
			}
			pw, err := c.secret("Password")
			if err != nil {
				return err
			}
			again, err := c.secret("Password again")
			if err != nil {
				return err
			}
			if pw != again {
				c.printf("  %s the passwords differ\n", c.pal.Cross())
				continue
			}
			if err := c.H.Setup(user, pw); err != nil {
				c.printf("  %s %v\n", c.pal.Cross(), err)
				continue
			}
			return nil
		}
	}
	var last error
	for i := 0; i < 3; i++ {
		user, err := c.ask("User", "")
		if err != nil {
			return err
		}
		pw, err := c.secret("Password")
		if err != nil {
			return err
		}
		if last = c.H.Login(user, pw); last == nil {
			return nil
		}
		c.printf("  %s %v\n", c.pal.Cross(), last)
	}
	return last
}

// OnInterrupt handles Ctrl+C: while a job is shown it asks the hub to stop
// after the running step and reports true; otherwise the caller exits.
func (c *Console) OnInterrupt() bool {
	id := c.following.Load()
	if id == nil {
		c.echoOn()
		return false
	}
	c.H.Jobs.Cancel(*id)
	return true
}

// ---------------------------------------------------------------- input

func (c *Console) printf(format string, a ...any) { fmt.Fprintf(c.out, format, a...) }

// line reads one typed line without control characters.
func (c *Console) line() (string, error) {
	raw, err := c.in.ReadString('\n')
	clean, _ := ui.CleanInput(raw)
	if err != nil && strings.TrimSpace(clean) == "" {
		return "", io.EOF
	}
	return strings.TrimSpace(clean), nil
}

// ask prints a question and reads the answer; empty input returns def.
func (c *Console) ask(question, def string) (string, error) {
	if def != "" {
		c.printf("  %s [%s]: ", question, def)
	} else {
		c.printf("  %s: ", question)
	}
	s, err := c.line()
	if !c.TTY {
		c.printf("%s\n", s)
	}
	if err != nil {
		return "", err
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}

// yes asks a Y/N question; anything but y/e(vet) is no.
func (c *Console) yes(question string) (bool, error) {
	c.printf("\n  %s  %s %s  %s %s  › ", question, c.pal.Paint(ui.Bold, "[Y]"), "Yes", c.pal.Paint(ui.Bold, "[N]"), "No")
	s, err := c.line()
	if !c.TTY {
		c.printf("%s\n", s)
	}
	if err != nil {
		return false, err
	}
	s = strings.ToLower(s)
	return s == "y" || s == "yes" || s == "e" || s == "evet", nil
}

// secret reads a password; on a terminal the typed characters are not shown.
func (c *Console) secret(prompt string) (string, error) {
	c.printf("  %s: ", prompt)
	if c.TTY {
		_ = exec.Command("stty", "-echo").Run()
		defer func() { c.echoOn(); c.printf("\n") }()
	} else {
		defer c.printf("\n")
	}
	raw, err := c.in.ReadString('\n')
	if err != nil && raw == "" {
		return "", io.EOF
	}
	return strings.TrimRight(raw, "\r\n"), nil
}

func (c *Console) echoOn() {
	if c.TTY {
		_ = exec.Command("stty", "echo").Run()
	}
}

// pick lets the operator choose one of names by number or by name.
func (c *Console) pick(question string, names []string) (int, error) {
	for i, n := range names {
		c.printf("    %s %s\n", c.pal.Paint(ui.Bold, fmt.Sprintf("%2d)", i+1)), n)
	}
	for {
		s, err := c.ask(question+" (number or name, Enter = back)", "")
		if err != nil || s == "" {
			return -1, err
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(names) {
			return n - 1, nil
		}
		for i, n := range names {
			if strings.EqualFold(strings.Fields(ui.Strip(n))[0], s) {
				return i, nil
			}
		}
		c.printf("  %s no such entry: %s\n", c.pal.Paint(ui.Yellow, "!"), s)
	}
}

// numbers parses "1,3-4" against n entries.
func numbers(s string, n int) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		b := a
		if err == nil && isRange {
			b, err = strconv.Atoi(strings.TrimSpace(hi))
		}
		if err != nil || a < 1 || b > n || a > b {
			return nil, fmt.Errorf("%q is not a number or range between 1 and %d", part, n)
		}
		for i := a; i <= b; i++ {
			if !seen[i] {
				seen[i] = true
				out = append(out, i-1)
			}
		}
	}
	return out, nil
}
