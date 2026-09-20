package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

func exampleReport() *status.Report {
	k := kernel.Version{Release: 793, Patch: 200, Changelist: 2123456, Platform: "linuxx86_64",
		CompilationMode: "UNICODE", CompiledFor: "64 BIT", CompileTime: "Sep 14 2026 21:33:27",
		SupportedBasis: []string{"755", "756", "757", "758"}}
	return &status.Report{
		GeneratedAt: time.Date(2026, 9, 20, 19, 40, 12, 0, time.UTC),
		Host: status.HostInfo{Hostname: "sapci", OS: "linux", OSVersion: "SUSE Linux Enterprise Server 15 SP5 (kernel 5.14.21)",
			Arch: "amd64", KernelDirName: "linuxx86_64", User: "abcadm"},
		HostAgent: discovery.HostAgent{Installed: true, Running: true, Path: "/usr/sap/hostctrl/exe/saphostexec",
			Version: kernel.Version{Release: 722, Patch: 65}},
		Systems: []status.System{{
			SID: "ABC", Type: "ABAP", Status: "YELLOW",
			Database:     status.DB{Type: "hdb", Name: "HDB", Host: "saphdb"},
			Kernel:       k,
			KernelSource: "disp+work -V (/usr/sap/ABC/SYS/exe/uc/linuxx86_64)",
			DirExeRoot:   "/usr/sap/ABC/SYS/exe", DirCtRun: "/usr/sap/ABC/SYS/exe/uc/linuxx86_64",
			GlobalKernelDir: "/sapmnt/ABC/exe/uc/linuxx86_64",
			Instances: []status.Instance{
				{Nr: "00", Name: "D00", Type: "D", TypeDesc: "Application Server (ABAP)", Host: "sapci", Local: true,
					Profile: "/usr/sap/ABC/SYS/profile/ABC_D00_sapci", DirExecutable: "/usr/sap/ABC/D00/exe",
					Sapstartsrv: "running", Status: "GREEN", Processes: []sapcontrol.Process{{Name: "disp+work", DispStatus: "GREEN"}}},
				{Nr: "01", Name: "ASCS01", Type: "ASCS", TypeDesc: "ABAP Central Services", Host: "sapci", Local: true,
					Profile: "/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci", DirExecutable: "/usr/sap/ABC/ASCS01/exe",
					Sapstartsrv: "running", Status: "GREEN"},
				{Nr: "02", Type: "D", TypeDesc: "Application Server (ABAP)", Host: "sapapp2", Sapstartsrv: "remote", Status: "GRAY"},
			},
		}, {
			SID: "QAS", Type: "ABAP", Status: "GRAY",
			Database:     status.DB{Type: "hdb", Name: "HDQ", Host: "saphdq"},
			Kernel:       kernel.Version{Release: 793, Patch: 150, Platform: "linuxx86_64"},
			KernelSource: "saphostctrl ListInstances",
			Instances: []status.Instance{
				{Nr: "10", Name: "ASCS10", Type: "ASCS", TypeDesc: "ABAP Central Services", Host: "sapci", Local: true,
					Profile: "/usr/sap/QAS/SYS/profile/QAS_ASCS10_sapci", DirExecutable: "/usr/sap/QAS/ASCS10/exe",
					Sapstartsrv: "not running", Status: "GRAY"},
				{Nr: "11", Name: "D11", Type: "D", TypeDesc: "Application Server (ABAP)", Host: "sapci", Local: true,
					Profile: "/usr/sap/QAS/SYS/profile/QAS_D11_sapci", DirExecutable: "/usr/sap/QAS/D11/exe",
					Sapstartsrv: "not running", Status: "GRAY"},
			},
		}},
		Warnings: []string{"/usr/sap/sapservices: permission denied"},
	}
}

func TestRenderStatus(t *testing.T) {
	var buf bytes.Buffer
	RenderStatus(&buf, exampleReport(), ui.Palette{})
	out := buf.String()
	for _, want := range []string{"SYSTEM ABC · ABAP · (~) YELLOW", "Kernel version               793 (7.93)",
		"Kernel patch level           200 (changelist 2123456)", "DIR_CT_RUN                   /usr/sap/ABC/SYS/exe/uc/linuxx86_64",
		"Global kernel directory      /sapmnt/ABC/exe/uc/linuxx86_64", "ASCS01", "sapapp2", "remote", "SAP HANA (hdb)",
		"755–758", "SAP Host Agent               (+) 722 patch 65", "WARNINGS"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
	// Refresh the documented example screen when requested: KERNELMAN_WRITE_EXAMPLE=1 go test ./internal/cli
	if os.Getenv("KERNELMAN_WRITE_EXAMPLE") == "1" {
		var colour bytes.Buffer
		RenderStatus(&colour, exampleReport(), ui.Palette{Colour: true, Unicode: true})
		content := append([]byte("$ ./kernelman.sh status\n"), colour.Bytes()...)
		if err := os.WriteFile("../../docs/examples/status-linux.txt", content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
