package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// Process-wide wiring. Tests and the menu replace these.
var (
	stdout  io.Writer     = os.Stdout
	input   *bufio.Reader = bufio.NewReader(os.Stdin)
	runner  exec.Runner   = exec.NewReal()
	plat    platform.Platform
	palette *ui.Palette
	isRoot  = os.Geteuid() == 0
	// echoInput repeats what was typed so transcripts (piped input) show the
	// answers; a terminal already echoes, and raw input is never written back.
	echoInput = !ui.IsTerminal(os.Stdin)
	// defaultDownloadDir is offered when no download directory is remembered ("" = current directory).
	defaultDownloadDir string
)

// SetOutput redirects every screen written by the operations (default os.Stdout).
func SetOutput(w io.Writer) { stdout = w }

func platformNow() platform.Platform {
	if plat == nil {
		plat = platform.Current()
	}
	return plat
}

func currentPalette() ui.Palette {
	if palette != nil {
		return *palette
	}
	return ui.Detect(os.Stdout)
}

// collectStatus gathers the live report; tests replace it.
var collectStatus = func(ctx context.Context, opts status.Options) *status.Report {
	return status.Collect(ctx, runner, platformNow(), opts)
}

// resolveTarget finds the system to operate on; tests replace it.
var resolveTarget = func(ctx context.Context, sid string) (*system.Target, []string, error) {
	return system.Resolve(ctx, runner, platformNow(), sid)
}

// readLine reads one typed line with control and escape bytes removed and
// reports whether Esc was pressed. Only the cleaned text is ever echoed.
func readLine() (line string, esc bool, err error) {
	raw, err := input.ReadString('\n')
	line, esc = ui.CleanInput(raw)
	if echoInput {
		fmt.Fprintln(stdout, line)
	} else if esc || err == nil {
		fmt.Fprintln(stdout) // the terminal echoed the keys; finish the line
	}
	return line, esc, err
}

// ask prints a question and reads one line; empty input or Esc returns def.
func ask(question, def string) string {
	if def != "" {
		fmt.Fprintf(stdout, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(stdout, "%s: ", question)
	}
	line, esc, _ := readLine()
	if line == "" || esc {
		return def
	}
	return line
}

// choice is one key of choose: the letter the user presses and its label.
type choice struct {
	Key     string // "Y", "N", "S", "K", "M", "Q"
	Label   string
	Aliases []string
}

// choose shows the keys inline, e.g. "[Y] Yes  [N] No  ›", and reads until
// one is pressed (letters, case-insensitive; Enter alone re-asks; Esc takes
// the cancelling option: No, else Main menu, else the last one).
func choose(question string, opts ...choice) string {
	pal := currentPalette()
	for {
		var parts []string
		for _, o := range opts {
			parts = append(parts, pal.Paint(ui.Bold, "["+o.Key+"]")+" "+o.Label)
		}
		if question != "" {
			fmt.Fprintf(stdout, "\n  %s  ", question)
		} else {
			fmt.Fprint(stdout, "\n  ")
		}
		fmt.Fprintf(stdout, "%s  %s ", strings.Join(parts, "  "), pal.Paint(ui.Dim, "›"))
		line, esc, err := readLine()
		in := strings.ToLower(line)
		if esc {
			return cancelKey(opts)
		}
		for _, o := range opts {
			if in == strings.ToLower(o.Key) {
				return o.Key
			}
			for _, a := range o.Aliases {
				if in == a {
					return o.Key
				}
			}
		}
		if err != nil { // end of input: take the last (safe) option
			return opts[len(opts)-1].Key
		}
	}
}

// cancelKey is the option Esc selects.
func cancelKey(opts []choice) string {
	for _, k := range []string{"N", "M"} {
		for _, o := range opts {
			if o.Key == k {
				return k
			}
		}
	}
	return opts[len(opts)-1].Key
}

var (
	yes      = choice{"Y", "Yes", []string{"yes", "e", "evet"}}
	no       = choice{"N", "No", []string{"no", "h", "hayır", "hayir"}}
	mainMenu = choice{"M", "Main menu", []string{"menu", "main", "0", "b", "back"}}
	quit     = choice{"Q", "Quit", []string{"quit", "exit"}}
)

// confirm asks a yes/no question answered with Y or N.
func confirm(question string) bool { return choose(question, yes, no) == "Y" }

// progress renders ops.Progress with colours.
type progress struct {
	w   io.Writer
	pal ui.Palette
}

func (p progress) Begin(i, n int, title string) {
	fmt.Fprintf(p.w, "  [%d/%d] %s ... ", i, n, title)
}

func (p progress) End(err error, detail string, d time.Duration) {
	dur := ""
	if d >= time.Second {
		dur = fmt.Sprintf(" (%s)", d.Round(time.Second))
	}
	if err != nil {
		fmt.Fprintf(p.w, "%s %s%s\n", p.pal.Cross(), p.pal.Paint(ui.Red, err.Error()), dur)
		return
	}
	if detail != "" {
		detail = "  " + p.pal.Paint(ui.Dim, detail)
	}
	fmt.Fprintf(p.w, "%s%s%s\n", p.pal.Paint(ui.Green, "ok"), detail, dur)
}

func (p progress) Info(line string) { fmt.Fprintf(p.w, "  %s\n", p.pal.Paint(ui.Dim, line)) }

func (p progress) Block(title, text string) {
	fmt.Fprintf(p.w, "\n  %s\n", p.pal.Paint(ui.Cyan, title))
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(p.w, "    %s\n", l)
	}
	fmt.Fprintln(p.w)
}

// newEnv builds the ops environment for a resolved target.
func newEnv(t *system.Target) *ops.Env {
	user := currentUser()
	if demoRoot != "" {
		user = t.SIDAdm // the demo behaves like <sid>adm on a real host
	}
	return &ops.Env{R: runner, P: platformNow(), T: t, Pr: progress{w: stdout, pal: currentPalette()}, IsRoot: isRoot, User: user}
}

// target resolves the system, asking for the SID when the host runs several.
func target(ctx context.Context, sid string) (*system.Target, error) {
	t, warnings, err := resolveTarget(ctx, sid)
	var multi *system.ErrMultipleSIDs
	if err != nil && errorsAs(err, &multi) {
		sid = ask("Several SAP systems found ("+strings.Join(multi.SIDs, ", ")+"). Which one?", multi.SIDs[0])
		t, warnings, err = resolveTarget(ctx, sid)
	}
	pal := currentPalette()
	for _, w := range warnings {
		fmt.Fprintf(stdout, "  %s %s\n", pal.Paint(ui.Yellow, "!"), w)
	}
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(stdout, "  System %s · %s\n  Kernel directories:\n", pal.Paint(ui.Bold, t.SID), runAsNote(t, pal))
	for i, d := range kernelDirs(t) {
		note := ""
		if i == 0 {
			note = pal.Paint(ui.Dim, "  central · "+t.Source)
		}
		fmt.Fprintf(stdout, "    %s%s\n", d, note)
	}
	fmt.Fprintln(stdout)
	return t, nil
}

// runAsNote explains which user performs the file operations.
func runAsNote(t *system.Target, pal ui.Palette) string {
	me := currentUser()
	switch {
	case demoRoot != "":
		return fmt.Sprintf("running as %s · demo: acting as %s", me, t.SIDAdm)
	case isRoot:
		return fmt.Sprintf("running as root → file operations and sapcontrol via su - %s", t.SIDAdm)
	case me == t.SIDAdm:
		return fmt.Sprintf("running as %s %s (new files belong to %s:%s)", me, pal.Check(), t.SIDAdm, t.Group)
	}
	return pal.Paint(ui.Yellow, fmt.Sprintf("! running as %s: not root and not %s — files will belong to %s; run chown -R %s:%s as root afterwards",
		me, t.SIDAdm, me, t.SIDAdm, t.Group))
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" && !isRoot {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

// fail prints an error in red and returns the generic exit code.
func fail(err error) int {
	fmt.Fprintf(stdout, "\n  %s %s\n", currentPalette().Cross(), currentPalette().Paint(ui.Red, err.Error()))
	return ExitError
}

// lights prints the system state as a big badge plus one light per instance.
func lights(ctx context.Context, e *ops.Env) bool {
	pal := currentPalette()
	states := ops.Probe(ctx, e)
	var parts []string
	var agg []string
	for _, st := range states {
		switch {
		case !st.Sapstartsrv:
			parts = append(parts, fmt.Sprintf("%s %s (sapstartsrv down)", pal.Light(ui.Red), st.Name))
			agg = append(agg, "GRAY")
		default:
			parts = append(parts, fmt.Sprintf("%s %s %s", statusLight(pal, st.Status), st.Name, st.Status))
			agg = append(agg, st.Status)
		}
	}
	sys := sapcontrol.Aggregate(agg)
	fmt.Fprintf(stdout, "  %s   %s\n", pal.Badge(statusColour(sys), stateWord(sys)), strings.Join(parts, "   "))
	return ops.IsStopped(states)
}
