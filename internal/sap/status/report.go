// Package status collects the "current state" screen: host, SAP Host Agent,
// and for every SAP system on the host its kernel, directories and instances.
package status

import (
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
)

// Report is the full status snapshot; it is rendered as text or JSON.
type Report struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Host        HostInfo            `json:"host"`
	HostAgent   discovery.HostAgent `json:"host_agent"`
	Systems     []System            `json:"systems"`
	Warnings    []string            `json:"warnings,omitempty"`
}

// HostInfo describes the machine kernelman runs on.
type HostInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	OSVersion     string `json:"os_version"`
	Arch          string `json:"arch"`
	KernelDirName string `json:"kernel_dir_name"` // SAP platform directory expected on this host
	User          string `json:"user"`
	Privileged    bool   `json:"privileged"`
}

// System is one SAP system (SID) as seen from this host.
type System struct {
	SID             string         `json:"sid"`
	Type            string         `json:"type"` // ABAP | Java | Dual-stack | HANA | unknown
	Database        DB             `json:"database"`
	Kernel          kernel.Version `json:"kernel"`
	KernelSource    string         `json:"kernel_source"` // how the kernel version was determined
	DirExeRoot      string         `json:"dir_exe_root,omitempty"`
	DirCtRun        string         `json:"dir_ct_run,omitempty"`
	GlobalKernelDir string         `json:"global_kernel_dir,omitempty"` // DIR_CT_RUN with symlinks resolved
	Status          string         `json:"status"`                      // aggregate GREEN/YELLOW/RED/GRAY
	Instances       []Instance     `json:"instances"`
	Errors          []string       `json:"errors,omitempty"`
}

// DB identifies the system's database.
type DB struct {
	Type string `json:"type,omitempty"` // hdb, ora, db6, mss, syb, ada
	Name string `json:"name,omitempty"`
	Host string `json:"host,omitempty"`
}

// Instance is one instance of a system, local or remote.
type Instance struct {
	Nr            string               `json:"nr"`
	Name          string               `json:"name,omitempty"`
	Type          string               `json:"type,omitempty"`
	TypeDesc      string               `json:"type_desc,omitempty"`
	Host          string               `json:"host"`
	Local         bool                 `json:"local"`
	Profile       string               `json:"profile,omitempty"`
	DirExecutable string               `json:"dir_executable,omitempty"` // local kernel directory
	Features      []string             `json:"features,omitempty"`
	Sapstartsrv   string               `json:"sapstartsrv"` // running | not running | remote | unknown
	Status        string               `json:"status"`
	Processes     []sapcontrol.Process `json:"processes,omitempty"`
	Error         string               `json:"error,omitempty"`
}

var dbNames = map[string]string{
	"hdb": "SAP HANA", "ora": "Oracle", "db6": "IBM DB2 LUW", "mss": "Microsoft SQL Server",
	"syb": "SAP ASE", "ada": "SAP MaxDB", "db4": "IBM DB2 for i", "db2": "IBM DB2 for z/OS",
}

// DisplayName returns "SAP HANA (hdb)".
func (d DB) DisplayName() string {
	if d.Type == "" {
		return "unknown"
	}
	if n, ok := dbNames[d.Type]; ok {
		return n + " (" + d.Type + ")"
	}
	return d.Type
}
