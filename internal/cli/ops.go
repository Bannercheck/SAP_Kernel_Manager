package cli

import (
	"fmt"
	"io"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// Op is one user-facing operation. Name is what people see in the menu,
// help and progress lines; ID is the sub-command on the command line.
type Op struct {
	ID      string
	Name    string
	Summary string
	Menu    bool // shown in the interactive menu
	Run     func([]string) int
}

// Ops is the single registry of operations, in menu order.
var Ops = []Op{
	{ID: "status", Name: "SAP Status", Summary: "kernel version, sapstartsrv/sapcontrol, SID, hostname, instances", Menu: true, Run: Status},
	{ID: "backup", Name: "Kernel Backup", Summary: "copy the kernel directory to exe_<date> and list it", Menu: true, Run: BackupOp},
	{ID: "files", Name: "Kernel Files", Summary: "copy today's *.SAR from the download directory into the kernel directory, chown", Menu: true, Run: FilesOp},
	{ID: "control", Name: "SAP Stop / Start", Summary: "K = stop the system, S = start it", Menu: true, Run: ControlOp},
	{ID: "update", Name: "Kernel Update", Summary: "extract the archives in ascending patch order, chown, saproot.sh, verify, start", Menu: true, Run: UpdateOp},
	{ID: "rollback", Name: "Kernel Rollback", Summary: "copy the latest exe_<date> backup back over the kernel directory", Menu: true, Run: RollbackOp},
	{ID: "stop", Name: "SAP Stop", Summary: "stop the system (command line)", Run: StopOp},
	{ID: "start", Name: "SAP Start", Summary: "start the system (command line)", Run: StartOp},
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

// Dispatch runs the operation.
func Dispatch(op Op, args []string) int { return op.Run(args) }

// Usage prints the top-level help.
func Usage(w io.Writer) {
	fmt.Fprintf(w, "%s — %s (Unix)\n\nUsage: %s [command] [flags]     no command on a terminal = interactive menu\n\n",
		version.DisplayName, version.ProductName, version.AppName)
	for _, op := range Ops {
		fmt.Fprintf(w, "  %-9s %-17s %s\n", op.ID, op.Name, op.Summary)
	}
	fmt.Fprint(w, `
Common flags: --sid SID   (needed only when the host runs several systems)
  status   --output table|json
  files    --from DIR [--yes]
  update   [--from DIR] [--yes] [--start]
  rollback [--backup DIR] [--yes]
`)
}
