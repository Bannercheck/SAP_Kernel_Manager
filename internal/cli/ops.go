package cli

import (
	"fmt"
	"io"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// Op is one user-facing operation. Name is what people see in the menu,
// help, progress lines and journals; ID is the sub-command on the command line.
type Op struct {
	ID      string
	Name    string // "SAP Status", "Kernel Backup" ...
	Summary string
	Step    int                // roadmap step that delivers it (0 = available)
	Run     func([]string) int // nil while planned
}

// Ops is the single registry of operations, in menu order.
var Ops = []Op{
	{ID: "status", Name: "SAP Status", Summary: "host, kernel, directories, instances and sapstartsrv state", Run: Status},
	{ID: "stop", Name: "SAP Stop", Summary: "stop the SAP system and wait until every instance is down", Step: 2},
	{ID: "start", Name: "SAP Start", Summary: "start sapstartsrv and the SAP system, wait until running", Step: 2},
	{ID: "backup", Name: "Kernel Backup", Summary: "copy DIR_CT_RUN to exe_<timestamp> as <sid>adm:sapsys", Step: 3},
	{ID: "files", Name: "Kernel Files", Summary: "scan the download directory: archives, apply order, target patch level", Step: 4},
	{ID: "update", Name: "Kernel Update", Summary: "ask for the download directory, show the plan, confirm, then update", Step: 5},
	{ID: "rollback", Name: "Kernel Rollback", Summary: "restore the backup taken by a previous update", Step: 6},
	{ID: "history", Name: "Run History", Summary: "previous runs and their outcome", Step: 6},
	{ID: "doctor", Name: "Health Check", Summary: "tool paths, privileges, sapcontrol access", Step: 5},
	{ID: "version", Name: "Version", Summary: "build information", Run: Version},
}

// FindOp returns the operation for a sub-command ID.
func FindOp(id string) (Op, bool) {
	for _, op := range Ops {
		if op.ID == id {
			return op, true
		}
	}
	return Op{}, false
}

// Dispatch runs the operation or explains that it is not available yet.
func Dispatch(op Op, args []string) int {
	if op.Run == nil {
		fmt.Fprintf(stdout, "%s (%s %s) is planned for step %d and not available yet.\n", op.Name, version.AppName, op.ID, op.Step)
		return ExitError
	}
	return op.Run(args)
}

// Usage prints the top-level help.
func Usage(w io.Writer) {
	fmt.Fprintf(w, "%s — %s\n\nUsage: %s <command> [flags]      (no arguments on a terminal opens the menu)\n\n",
		version.DisplayName, version.ProductName, version.AppName)
	for _, op := range Ops {
		state := ""
		if op.Run == nil {
			state = fmt.Sprintf("  [planned: step %d]", op.Step)
		}
		fmt.Fprintf(w, "  %-9s %-16s %s%s\n", op.ID, op.Name, op.Summary, state)
	}
	fmt.Fprint(w, "\nFlags for status: --sid SID  --output table|json  --timeout 30s\n"+
		"Planned: update --from <download dir> [--dry-run] [--yes]\n")
}
