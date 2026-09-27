package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
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
)

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

// ask prints a question and reads one line; empty input returns def.
func ask(question, def string) string {
	if def != "" {
		fmt.Fprintf(stdout, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(stdout, "%s: ", question)
	}
	line, _ := input.ReadString('\n')
	line = strings.TrimSpace(line)
	fmt.Fprintln(stdout, line)
	if line == "" {
		return def
	}
	return line
}

// confirm asks a yes/no question; Enter means no unless defYes.
func confirm(question string, defYes bool) bool {
	def := "y/N"
	if defYes {
		def = "Y/n"
	}
	a := strings.ToLower(ask(question, def))
	if a == strings.ToLower(def) {
		return defYes
	}
	return a == "y" || a == "yes" || a == "e" || a == "evet"
}

// key reads a single-letter choice (first character, upper-cased).
func key(question string) string {
	a := strings.TrimSpace(ask(question, ""))
	if a == "" {
		return ""
	}
	return strings.ToUpper(a[:1])
}

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
	return &ops.Env{R: runner, P: platformNow(), T: t, Pr: progress{w: stdout, pal: currentPalette()}, IsRoot: isRoot}
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
	fmt.Fprintf(stdout, "  System %s · user %s · kernel dir %s\n  %s\n\n", pal.Paint(ui.Bold, t.SID), t.SIDAdm, t.KernelDir,
		pal.Paint(ui.Dim, "("+t.Source+")"))
	return t, nil
}

// fail prints an error in red and returns the generic exit code.
func fail(err error) int {
	fmt.Fprintf(stdout, "\n  %s %s\n", currentPalette().Cross(), currentPalette().Paint(ui.Red, err.Error()))
	return ExitError
}

// lights prints one line per local instance with its traffic light.
func lights(ctx context.Context, e *ops.Env) bool {
	pal := currentPalette()
	states := ops.Probe(ctx, e)
	var parts []string
	for _, st := range states {
		switch {
		case !st.Sapstartsrv:
			parts = append(parts, fmt.Sprintf("%s %s (sapstartsrv down)", pal.Light(ui.Red), st.Name))
		default:
			parts = append(parts, fmt.Sprintf("%s %s %s", statusLight(pal, st.Status), st.Name, st.Status))
		}
	}
	fmt.Fprintf(stdout, "  %s\n", strings.Join(parts, "   "))
	return ops.IsStopped(states)
}
