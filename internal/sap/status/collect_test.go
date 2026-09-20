package status

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// fakePlatform is a Linux-like platform whose paths point into a temp dir.
type fakePlatform struct{ sapservices string }

func (fakePlatform) Name() string                 { return "linux" }
func (fakePlatform) Arch() string                 { return "amd64" }
func (fakePlatform) KernelDirName() string        { return "linuxx86_64" }
func (fakePlatform) ExeSuffix() string            { return "" }
func (fakePlatform) LibPathVar() string           { return "LD_LIBRARY_PATH" }
func (fakePlatform) SIDAdmUser(sid string) string { return strings.ToLower(sid) + "adm" }
func (fakePlatform) IsPrivileged() bool           { return false }
func (fakePlatform) HostctrlExeDir() string       { return "/nonexistent/hostctrl/exe" }
func (f fakePlatform) SapservicesPath() string    { return f.sapservices }
func (fakePlatform) OSVersion(context.Context, exec.Runner) string {
	return "SUSE Linux Enterprise Server 15 SP5"
}

func read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func param(value string) string {
	return "\n20.09.2026 19:30:04\nParameterValue\nOK\n" + value + "\n"
}

// newScenario scripts a host with ABAP system ABC (D00 + ASCS01 local,
// D02 remote) and a HANA instance HDB/02 whose sapstartsrv is down.
func newScenario(t *testing.T) (*exec.Fake, fakePlatform, string) {
	t.Helper()
	dir := t.TempDir()
	ctRun := filepath.Join(dir, "exe", "uc", "linuxx86_64")
	if err := os.MkdirAll(ctRun, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctRun, "disp+work"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sapservices := filepath.Join(dir, "sapservices")
	if err := os.WriteFile(sapservices, []byte(read(t, "discovery/testdata/linux/sapservices")), 0o644); err != nil {
		t.Fatal(err)
	}

	f := exec.NewFake()
	f.Paths["saphostctrl"] = "/hc/saphostctrl"
	f.Paths["saphostexec"] = "/hc/saphostexec"
	f.Paths["sapcontrol"] = "/hc/sapcontrol"
	f.On("/hc/saphostctrl -function ListInstances", read(t, "discovery/testdata/linux/ListInstances.txt"), 0).
		On("/hc/saphostexec -status", "saphostexec running (pid = 3456)\nsapstartsrv running (pid = 3457)\n", 0).
		On("/hc/saphostexec -version", read(t, "kernel/testdata/linux/saphostexec_version.txt"), 0).
		On(filepath.Join(ctRun, "disp+work")+" -V", read(t, "kernel/testdata/linux/dispwork_V.txt"), 0)

	sc := func(nr, fn, file string, exit int) {
		f.On("/hc/sapcontrol -nr "+nr+" -function "+fn, read(t, "sapcontrol/testdata/linux/"+file), exit)
	}
	sc("00", "GetProcessList", "GetProcessList.txt", 3)
	sc("00", "GetInstanceProperties", "GetInstanceProperties.txt", 0)
	sc("00", "GetSystemInstanceList", "GetSystemInstanceList.txt", 0)
	sc("01", "GetProcessList", "GetProcessList_ASCS.txt", 3)
	sc("02", "GetProcessList", "GetProcessList_FAIL.txt", 1)
	f.On("/hc/sapcontrol -nr 00 -function ParameterValue DIR_EXECUTABLE", param("/usr/sap/ABC/D00/exe"), 0).
		On("/hc/sapcontrol -nr 01 -function ParameterValue DIR_EXECUTABLE", param("/usr/sap/ABC/ASCS01/exe"), 0).
		On("/hc/sapcontrol -nr 00 -function ParameterValue DIR_CT_RUN", param(ctRun), 0).
		On("/hc/sapcontrol -nr 00 -function ParameterValue DIR_EXE_ROOT", param("/usr/sap/ABC/SYS/exe"), 0).
		On("/hc/sapcontrol -nr 00 -function ParameterValue dbms/type", param("hdb"), 0).
		On("/hc/sapcontrol -nr 00 -function ParameterValue SAPDBHOST", param("saphdb"), 0).
		On("/hc/sapcontrol -nr 00 -function ParameterValue dbs/hdb/dbname", param("HDB"), 0)
	return f, fakePlatform{sapservices: sapservices}, ctRun
}

func TestCollect(t *testing.T) {
	f, p, ctRun := newScenario(t)
	rep := Collect(context.Background(), f, p, Options{})

	if !rep.HostAgent.Running || rep.HostAgent.Version.Release != 722 || rep.HostAgent.Version.Patch != 65 {
		t.Errorf("host agent = %+v", rep.HostAgent)
	}
	if len(rep.Systems) != 2 || rep.Systems[0].SID != "ABC" || rep.Systems[1].SID != "HDB" {
		t.Fatalf("systems = %+v", rep.Systems)
	}

	abc := rep.Systems[0]
	if abc.Type != "ABAP" || abc.Status != "YELLOW" || abc.Database.Type != "hdb" || abc.Database.Name != "HDB" ||
		abc.Database.Host != "saphdb" || abc.DirCtRun != ctRun || abc.DirExeRoot != "/usr/sap/ABC/SYS/exe" {
		t.Errorf("ABC = %+v", abc)
	}
	if abc.Kernel.Release != 793 || abc.Kernel.Patch != 200 || !strings.HasPrefix(abc.KernelSource, "disp+work -V") {
		t.Errorf("ABC kernel = %+v via %s", abc.Kernel, abc.KernelSource)
	}
	if len(abc.Instances) != 3 {
		t.Fatalf("ABC instances = %+v", abc.Instances)
	}
	d00, ascs, remote := abc.Instances[0], abc.Instances[1], abc.Instances[2]
	if d00.Name != "D00" || d00.Type != "D" || d00.Sapstartsrv != "running" || d00.Status != "GREEN" ||
		d00.DirExecutable != "/usr/sap/ABC/D00/exe" || len(d00.Processes) != 4 || len(d00.Features) != 4 {
		t.Errorf("D00 = %+v", d00)
	}
	if ascs.Name != "ASCS01" || ascs.Profile != "/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci" || ascs.Status != "GREEN" {
		t.Errorf("ASCS01 = %+v", ascs)
	}
	if remote.Local || remote.Host != "sapapp2" || remote.Nr != "02" || remote.Sapstartsrv != "remote" ||
		remote.Status != "GRAY" || remote.Type != "D" {
		t.Errorf("remote = %+v", remote)
	}

	hdb := rep.Systems[1]
	if hdb.Status != "GRAY" || len(hdb.Instances) != 1 || hdb.Instances[0].Sapstartsrv != "not running" ||
		hdb.Kernel.Release != 753 || hdb.Kernel.Patch != 1200 || hdb.KernelSource != "saphostctrl ListInstances" {
		t.Errorf("HDB = %+v", hdb)
	}
	if len(f.Calls) == 0 {
		t.Error("no commands executed")
	}
}

func TestCollectFilterSID(t *testing.T) {
	f, p, _ := newScenario(t)
	rep := Collect(context.Background(), f, p, Options{SID: "hdb"})
	if len(rep.Systems) != 1 || rep.Systems[0].SID != "HDB" {
		t.Errorf("systems = %+v", rep.Systems)
	}
}
