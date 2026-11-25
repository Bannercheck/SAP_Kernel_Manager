package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// The operator's screen (host kupa): GPP stopped with 5 → K, so its
// sapstartsrv keeps running on purpose; the Diagnostics Agent runs; the SAP
// Host Agent is down. The SERVICES block must not read as if GPP were up,
// and the hint must say what is down.
func TestServiceLinesStoppedSystem(t *testing.T) {
	rep := &status.Report{
		HostAgent: discovery.HostAgent{Installed: true, Processes: []string{"saphostexec stopped"}},
		Systems: []status.System{
			{SID: "CSA", Status: "GREEN", Instances: []status.Instance{
				{Nr: "98", Name: "SMDA98", Local: true, Sapstartsrv: "running", Status: "GREEN", Profile: "/usr/sap/CSA/SYS/profile/CSA_SMDA98_kupa"}}},
			{SID: "GPP", Status: "GRAY", Instances: []status.Instance{
				{Nr: "00", Name: "J00", Local: true, Sapstartsrv: "running", Status: "GRAY", Profile: "/usr/sap/GPP/SYS/profile/GPP_J00_kupa"},
				{Nr: "01", Name: "SCS01", Local: true, Sapstartsrv: "running", Status: "GRAY", Profile: "/usr/sap/GPP/SYS/profile/GPP_SCS01_kupa"}}},
		},
	}
	if os.Getenv("KERNELMAN_WRITE_EXAMPLE") == "1" { // the home screen's SERVICES block of host kupa after 5 → K on GPP
		colour := strings.Join(ServiceLines(rep, ui.Palette{Colour: true, Unicode: true}), "\n  ")
		_ = os.WriteFile("../../docs/examples/services-stopped.txt", []byte("$ ./kernelman.sh              # GPP 5 → K ile durduruldu (sapstartsrv bilerek açık), Host Agent kapalı\n  "+colour+"\n"), 0o644)
	}
	out := ui.Strip(strings.Join(ServiceLines(rep, ui.Palette{Unicode: true}), "\n"))
	for _, want := range []string{
		"SAP Host Agent is down → 5) SAP Stop / Start → F starts it",
		"INSTANCE    SAP        SAPSTARTSRV  PROFILE (pf=)",
		"CSA SMDA98  ● running  ● up         /usr/sap/CSA/SYS/profile/CSA_SMDA98_kupa",
		"GPP J00     ● stopped  ● up         /usr/sap/GPP/SYS/profile/GPP_J00_kupa",
		"GPP SCS01   ● stopped  ● up",
		"sapstartsrv stays up after a stop on purpose",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q\n%s", want, out)
		}
	}
}

// With sapstartsrv down nothing can be known about the processes.
func TestServiceLinesSapstartsrvDown(t *testing.T) {
	rep := &status.Report{Systems: []status.System{{SID: "QAS", Status: "GRAY", Instances: []status.Instance{
		{Nr: "10", Name: "ASCS10", Local: true, Sapstartsrv: "not running", Status: "GRAY"}}}}}
	out := ui.Strip(strings.Join(ServiceLines(rep, ui.Palette{Unicode: true}), "\n"))
	for _, want := range []string{"sapstartsrv of QAS ASCS10 is down → 5) SAP Stop / Start → F starts it", "QAS ASCS10  ○ unknown  ● down"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q\n%s", want, out)
		}
	}
}
