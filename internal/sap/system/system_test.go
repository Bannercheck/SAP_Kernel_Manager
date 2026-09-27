package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

type fakePlatform struct{ sapservices string }

func (fakePlatform) Name() string                                  { return "linux" }
func (fakePlatform) Arch() string                                  { return "amd64" }
func (fakePlatform) KernelDirName() string                         { return "linuxx86_64" }
func (fakePlatform) ExeSuffix() string                             { return "" }
func (fakePlatform) LibPathVar() string                            { return "LD_LIBRARY_PATH" }
func (fakePlatform) SIDAdmUser(sid string) string                  { return strings.ToLower(sid) + "adm" }
func (fakePlatform) IsPrivileged() bool                            { return false }
func (fakePlatform) HostctrlExeDir() string                        { return "/nonexistent" }
func (f fakePlatform) SapservicesPath() string                     { return f.sapservices }
func (fakePlatform) OSVersion(context.Context, exec.Runner) string { return "test" }

func TestResolveFallbacks(t *testing.T) {
	root := t.TempDir()
	UsrSap = root
	defer func() { UsrSap = "/usr/sap" }()
	kdir := filepath.Join(root, "ABC", "SYS", "exe", "uc", "linuxx86_64")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kdir, filepath.Join(root, "ABC", "SYS", "exe", "run")); err != nil {
		t.Fatal(err)
	}
	services := filepath.Join(root, "sapservices")
	os.WriteFile(services, []byte("systemctl start SAPABC_00 # sapstartsrv pf=/usr/sap/ABC/SYS/profile/ABC_D00_sapci\nsystemctl start SAPQAS_10 # sapstartsrv pf=/usr/sap/QAS/SYS/profile/QAS_D10_sapci\n"), 0o644)
	p := fakePlatform{sapservices: services}

	// several systems, none chosen
	f := exec.NewFake()
	f.Paths["sapcontrol"] = "/hc/sapcontrol"
	if _, _, err := Resolve(context.Background(), f, p, ""); err == nil || !strings.Contains(err.Error(), "ABC, QAS") {
		t.Fatalf("expected multiple-SID error, got %v", err)
	}

	// 1) sapcontrol answers
	f.On("/hc/sapcontrol -nr 00 -function ParameterValue DIR_CT_RUN", "\nx\nParameterValue\nOK\n"+kdir+"\n", 0).
		On("/hc/sapcontrol -nr 00 -function ParameterValue DIR_EXE_ROOT", "\nx\nParameterValue\nOK\n/usr/sap/ABC/SYS/exe\n", 0)
	tgt, _, err := Resolve(context.Background(), f, p, "abc")
	if err != nil || tgt.KernelDir != kdir || !strings.HasPrefix(tgt.Source, "sapcontrol") || tgt.SIDAdm != "abcadm" || len(tgt.Instances) != 1 {
		t.Fatalf("sapcontrol path: %+v %v", tgt, err)
	}
	if _, err := os.Stat(filepath.Join(root, "ABC", ".kernelman", "snapshot.json")); err != nil {
		t.Errorf("snapshot not written: %v", err)
	}

	// 2) sapstartsrv down → snapshot
	f2 := exec.NewFake()
	f2.Paths["sapcontrol"] = "/hc/sapcontrol"
	f2.On("/hc/sapcontrol -nr 00 -function ParameterValue DIR_CT_RUN", "\nx\nParameterValue\nFAIL: NIECONN_REFUSED (Connection refused)\n", 1)
	tgt, _, err = Resolve(context.Background(), f2, p, "ABC")
	if err != nil || tgt.KernelDir != kdir || !strings.HasPrefix(tgt.Source, "snapshot") {
		t.Fatalf("snapshot path: %+v %v", tgt, err)
	}

	// 3) no snapshot → SYS/exe/run symlink
	os.Remove(filepath.Join(root, "ABC", ".kernelman", "snapshot.json"))
	tgt, _, err = Resolve(context.Background(), f2, p, "ABC")
	if err != nil || tgt.KernelDir != kdir || !strings.Contains(tgt.Source, "symlink") {
		t.Fatalf("symlink path: %+v %v", tgt, err)
	}
}
