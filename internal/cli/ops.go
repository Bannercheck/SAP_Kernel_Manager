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
	{ID: "backup", Name: "Kernel Backup", Summary: "copy every kernel directory next to itself as <name>_<date>, list them", Menu: true, Run: BackupOp},
	{ID: "download", Name: "Kernel Download", Summary: "optional, needs internet: S-user login, find this kernel's archives at SAP, confirm, download", Menu: true, Run: DownloadOp},
	{ID: "files", Name: "Kernel File Transfer", Summary: "find today's *.SAR anywhere on this server, copy into every kernel directory, chown", Menu: true, Run: FilesOp},
	{ID: "control", Name: "SAP Stop / Start", Summary: "S = start the system, K = stop it", Menu: true, Run: ControlOp},
	{ID: "update", Name: "Kernel Update", Summary: "extract in ascending patch order in every kernel directory, chown, saproot.sh, verify, start", Menu: true, Run: UpdateOp},
	{ID: "rollback", Name: "Kernel Rollback", Summary: "copy each directory's latest <name>_<date> backup back", Menu: true, Run: RollbackOp},
	{ID: "ship", Name: "Send to Other Servers", Summary: "scp today's archives and KernelMan itself to other hosts, verify with cksum", Menu: true, Run: ShipOp},
	{ID: "services", Name: "SAP Services", Summary: "SAP Host Agent and sapstartsrv per instance with profiles (pf=); start what is down", Menu: true, Run: ServicesOp},
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
Demo:  kernelman demo            simulated SAP host under ~/.kernelman/demo (no SAP needed; try every operation)
       kernelman --demo status  one command against the simulated host

Common flags: --sid SID   (needed only when the host runs several systems)
  status   --output table|json
  download [--to DIR] [--user S00...] [--yes]                automatic search; --basket FILE | --url LINK as fallback
           (password: KERNELMAN_SUSER_PASSWORD; MFA code is asked on the terminal)
  files    --from DIR [--yes]
  update   [--from DIR] [--yes] [--start]
  rollback [--backup DIR] [--yes]
  ship     --hosts h1,h2 [--user LOGIN] [--to /usr/sap/download] [--program-only] [--yes]
`)
}
