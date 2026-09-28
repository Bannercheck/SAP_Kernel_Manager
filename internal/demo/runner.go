package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
)

const patchFile = ".demo_patch"

// Runner simulates the SAP tools with a small state machine (sapstartsrv up
// or down, processes running or stopped) and delegates everything else to
// the real runner. SAPCAR "extracts" by raising the kernel patch level that
// disp+work -V reports, so Kernel Update and Rollback show real effects.
type Runner struct {
	mu      sync.Mutex
	l       Layout
	real    exec.Runner
	up      map[string]bool // sapstartsrv answers
	running map[string]bool // processes GREEN
}

// NewRunner returns a simulated host that is up and running.
func NewRunner(l Layout, real exec.Runner) *Runner {
	r := &Runner{l: l, real: real, up: map[string]bool{}, running: map[string]bool{}}
	for _, in := range instances {
		r.up[in.Nr], r.running[in.Nr] = true, true
	}
	return r
}

// LookPath resolves the simulated tools, everything else for real.
func (r *Runner) LookPath(file string) (string, error) {
	switch file {
	case "sapcontrol", "saphostctrl", "saphostexec":
		return filepath.Join(r.l.HostctrlDir, file), nil
	case "SAPCAR":
		return filepath.Join(r.l.KernelDir, "SAPCAR"), nil
	}
	return r.real.LookPath(file)
}

// Run dispatches on the executable name.
func (r *Runner) Run(ctx context.Context, c exec.Cmd) (exec.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch filepath.Base(c.Path) {
	case "sapcontrol":
		return r.sapcontrol(c.Args), nil
	case "saphostctrl":
		var b strings.Builder
		for _, in := range instances {
			fmt.Fprintf(&b, " Inst Info : %s - %s - %s - 793, patch %d, changelist 2123456\n", SID, in.Nr, Host, r.patch())
		}
		return exec.Result{Stdout: b.String()}, nil
	case "saphostexec":
		if len(c.Args) > 0 && c.Args[0] == "-version" {
			return exec.Result{Stdout: "kernel release                722\n\ncompiled on                   Linux GNU for linuxx86_64\n\npatch number                  65\n"}, nil
		}
		return exec.Result{Stdout: "saphostexec running (pid = 4242)\nsapstartsrv running (pid = 4243)\nsaposcol running (pid = 4244)\n"}, nil
	case "disp+work":
		return exec.Result{Stdout: r.dispwork()}, nil
	case "SAPCAR":
		return r.sapcar(c)
	}
	r.mu.Unlock()
	defer r.mu.Lock()
	return r.real.Run(ctx, c)
}

func (r *Runner) patch() int {
	b, err := os.ReadFile(filepath.Join(r.l.KernelDir, patchFile))
	if err != nil {
		return basePatch
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (r *Runner) dispwork() string {
	return fmt.Sprintf("--------------------\ndisp+work information\n--------------------\n\nkernel release                793\n\n"+
		"kernel make variant           793_REL\n\nDBMS client library           SQLDBC 2.20.14 (demo)\n\n"+
		"compiled on                   Linux GNU SLES-12 x86_64 cc9.3.1 for linuxx86_64\n\ncompiled for                  64 BIT\n\n"+
		"compilation mode              UNICODE\n\ncompile time                  Sep 14 2026 21:33:27\n\npatch number                  %d\n\n"+
		"database (SAP, table SVERS)   755\n                              756\n                              757\n                              758\n", r.patch())
}

// sapcar simulates -xvf: it lists the files an archive of that component
// would contain and records the new patch level.
func (r *Runner) sapcar(c exec.Cmd) (exec.Result, error) {
	if len(c.Args) < 2 || c.Args[0] != "-xvf" {
		return exec.Result{Stderr: "SAPCAR demo: only -xvf is simulated", ExitCode: 1}, nil
	}
	f, ok := ops.ParseSARName(filepath.Base(c.Args[1]))
	if !ok {
		return exec.Result{Stderr: "SAPCAR: cannot read archive " + c.Args[1], ExitCode: 1}, nil
	}
	files := []string{"disp+work"}
	if f.Full {
		files = kernelFiles
	}
	var b strings.Builder
	for _, n := range files {
		fmt.Fprintf(&b, "x %s\n", n)
	}
	if f.Component == "SAPEXE" || f.Component == "DW" {
		if f.Patch != r.patch() {
			_ = os.WriteFile(filepath.Join(r.l.KernelDir, patchFile), []byte(fmt.Sprint(f.Patch)), 0o644)
		}
	}
	return exec.Result{Stdout: b.String()}, nil
}

func body(fn string, lines ...string) string {
	return "\n" + time.Now().Format("02.01.2006 15:04:05") + "\n" + fn + "\nOK\n" + strings.Join(lines, "\n") + "\n"
}

func fail(fn, msg string) exec.Result {
	return exec.Result{Stdout: "\n" + time.Now().Format("02.01.2006 15:04:05") + "\n" + fn + "\nFAIL: " + msg + "\n", ExitCode: 1}
}

// sapcontrol interprets -nr NR -function FN [args].
func (r *Runner) sapcontrol(args []string) exec.Result {
	nr, fn := "", ""
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-nr":
			i++
			nr = args[i]
		case "-function":
			i++
			fn = args[i]
			rest = args[i+1:]
			i = len(args)
		}
	}
	in, known := r.instance(nr)
	if !known {
		return fail(fn, "NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()")
	}
	switch fn {
	case "StartService":
		r.up[nr] = true
		return exec.Result{Stdout: body(fn)}
	}
	if !r.up[nr] {
		return fail(fn, "NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()")
	}
	switch fn {
	case "GetProcessList":
		st, txt, exit := "GREEN", "Running", 3
		if !r.running[nr] {
			st, txt, exit = "GRAY", "Stopped", 4
		}
		rows := []string{"name, description, dispstatus, textstatus, starttime, elapsedtime, pid"}
		for _, p := range r.processes(in) {
			rows = append(rows, fmt.Sprintf("%s, %s, %s, %s, 2026 09 27 08:00:00, 1:00:00, 4711", p[0], p[1], st, txt))
		}
		return exec.Result{Stdout: body(fn, rows...), ExitCode: exit}
	case "GetInstanceProperties":
		return exec.Result{Stdout: body(fn, "property, propertytype, value", "SAPSYSTEM, Attribute, "+nr, "SAPSYSTEMNAME, Attribute, "+SID,
			"SAPLOCALHOST, Attribute, "+Host, "INSTANCE_NAME, Attribute, "+in.Name)}
	case "ParameterValue":
		return exec.Result{Stdout: body(fn, r.param(in, rest))}
	case "GetSystemInstanceList":
		rows := []string{"hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus"}
		for _, x := range instances {
			st := "GRAY"
			if r.up[x.Nr] && r.running[x.Nr] {
				st = "GREEN"
			}
			feat, prio := "ABAP|GATEWAY|ICMAN|IGS", "3"
			if x.Type == "CS" {
				feat, prio = "MESSAGESERVER|ENQUE", "1"
			}
			rows = append(rows, fmt.Sprintf("%s, %s, 5%s13, 5%s14, %s, %s, %s", Host, strings.TrimLeft(x.Nr, "0"), x.Nr, x.Nr, prio, feat, st))
		}
		return exec.Result{Stdout: body(fn, rows...)}
	case "GetVersionInfo":
		p := r.patch()
		rows := []string{"Filename, VersionInfo, Time, RKS Compatibility level"}
		for _, f := range []string{"sapstartsrv", "disp+work", "gwrd"} {
			rows = append(rows, fmt.Sprintf("%s/%s, 793, patch %d, changelist 2123456, RKS compatibility level 0, optU (Sep 14 2026, 21:33:27), linuxx86_64, 2026 09 14 21:33:27, 0",
				r.l.KernelDir, f, p))
		}
		return exec.Result{Stdout: body(fn, rows...)}
	case "StopSystem":
		for k := range r.running {
			r.running[k] = false
		}
	case "StartSystem":
		for k := range r.running {
			if r.up[k] {
				r.running[k] = true
			}
		}
	case "Start":
		r.running[nr] = true
	case "Stop":
		r.running[nr] = false
	case "StopService":
		r.up[nr] = false
	case "WaitforStopped", "WaitforStarted":
		// state already switched synchronously
	default:
		return fail(fn, "Invalid function")
	}
	return exec.Result{Stdout: body(fn)}
}

func (r *Runner) instance(nr string) (instance, bool) {
	for _, in := range instances {
		if in.Nr == nr {
			return in, true
		}
	}
	return instance{}, false
}

func (r *Runner) processes(in instance) [][2]string {
	if in.Type == "CS" {
		return [][2]string{{"msg_server", "MessageServer"}, {"enq_server", "Enqueue Server 2"}}
	}
	return [][2]string{{"disp+work", "Dispatcher"}, {"igswd_mt", "IGS Watchdog"}, {"gwrd", "Gateway"}, {"icman", "ICM"}}
}

func (r *Runner) param(in instance, args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "DIR_CT_RUN":
		return r.l.KernelDir
	case "DIR_EXE_ROOT":
		return filepath.Join(r.l.Root, SID, "SYS", "exe")
	case "DIR_EXECUTABLE":
		return filepath.Join(r.l.Root, SID, in.Name, "exe")
	case "SAPPROFILE":
		return filepath.Join(r.l.Root, SID, "SYS", "profile", fmt.Sprintf("%s_%s_%s", SID, in.Name, Host))
	case "dbms/type":
		return "hdb"
	case "SAPDBHOST":
		return "demo-db"
	case "dbs/hdb/dbname":
		return "HDB"
	}
	return ""
}
