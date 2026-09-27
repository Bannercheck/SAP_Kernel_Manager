// Package demo builds a simulated SAP host under a directory so that every
// KernelMan operation can be tried on a machine without SAP (a laptop).
// File operations (cp, ls, chown) are real; sapcontrol, saphostctrl,
// saphostexec, disp+work and SAPCAR are simulated by Runner.
package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
)

const (
	SID       = "ABC"
	Host      = "demo-host"
	basePatch = 200
)

var instances = []instance{{Nr: "00", Name: "D00", Type: "ABAP"}, {Nr: "01", Name: "ASCS01", Type: "CS"}}

type instance struct{ Nr, Name, Type string }

var kernelFiles = []string{"disp+work", "gwrd", "icman", "msg_server", "enq_server", "enq_replicator", "sapstartsrv", "sapcontrol",
	"sapcpe", "saproot.sh", "SAPCAR", "R3trans", "tp", "libsapu16.so", "libicuuc.so", "dbhdbslib.so"}

var todaySARs = []string{"SAPEXE_403-80007807.SAR", "SAPEXEDB_403-80007808.SAR", "dw_421-80007541.sar", "dw_423-80007541.sar"}
var oldSARs = []string{"SAPEXE_390-80007000.SAR", "SAPEXEDB_390-80007001.SAR"}

// Layout describes where the simulated host lives.
type Layout struct {
	Root        string // acts as /usr/sap
	KernelDir   string // DIR_CT_RUN
	HostctrlDir string
	Download    string
	Sapservices string
}

// Setup creates (or refreshes) the simulated host under root.
func Setup(root string, p platform.Platform) (Layout, error) {
	l := Layout{Root: root,
		KernelDir:   filepath.Join(root, SID, "SYS", "exe", "uc", p.KernelDirName()),
		HostctrlDir: filepath.Join(root, "hostctrl", "exe"),
		Download:    filepath.Join(root, "download"),
		Sapservices: filepath.Join(root, "sapservices")}
	dirs := []string{l.KernelDir, l.HostctrlDir, l.Download, filepath.Join(root, SID, "SYS", "profile")}
	for _, in := range instances {
		dirs = append(dirs, filepath.Join(root, SID, in.Name, "exe"))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return l, err
		}
	}
	for _, f := range kernelFiles {
		if err := stub(filepath.Join(l.KernelDir, f)); err != nil {
			return l, err
		}
	}
	if _, err := os.Stat(filepath.Join(l.KernelDir, patchFile)); err != nil {
		if err := os.WriteFile(filepath.Join(l.KernelDir, patchFile), []byte(fmt.Sprint(basePatch)), 0o644); err != nil {
			return l, err
		}
	}
	run := filepath.Join(root, SID, "SYS", "exe", "run")
	if _, err := os.Lstat(run); err != nil {
		_ = os.Symlink(l.KernelDir, run)
	}
	for _, f := range []string{"saphostctrl", "saphostexec", "sapcontrol"} {
		if err := stub(filepath.Join(l.HostctrlDir, f)); err != nil {
			return l, err
		}
	}
	var services string
	for _, in := range instances {
		profile := filepath.Join(root, SID, "SYS", "profile", fmt.Sprintf("%s_%s_%s", SID, in.Name, Host))
		if err := os.WriteFile(profile, []byte("SAPSYSTEMNAME = "+SID+"\nINSTANCE_NAME = "+in.Name+"\n"), 0o644); err != nil {
			return l, err
		}
		services += fmt.Sprintf("%s/sapstartsrv pf=%s -D -u abcadm\n", filepath.Join(root, SID, in.Name, "exe"), profile)
	}
	if err := os.WriteFile(l.Sapservices, []byte("#!/bin/sh\n"+services), 0o644); err != nil {
		return l, err
	}
	now := time.Now()
	for _, n := range todaySARs { // always dated today so the "today" filter finds them
		if err := archive(filepath.Join(l.Download, n), now); err != nil {
			return l, err
		}
	}
	for _, n := range oldSARs {
		if err := archive(filepath.Join(l.Download, n), now.Add(-96*time.Hour)); err != nil {
			return l, err
		}
	}
	return l, nil
}

func stub(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("#!/bin/sh\n# KernelMan demo stub\nexit 0\n"), 0o755)
}

func archive(path string, mt time.Time) error {
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, []byte("KernelMan demo archive "+filepath.Base(path)+"\n"), 0o644); err != nil {
			return err
		}
	}
	return os.Chtimes(path, mt, mt)
}

// Platform points discovery at the simulated host while keeping the real OS behaviour.
type Platform struct {
	platform.Platform
	l Layout
}

// NewPlatform wraps p for the layout.
func NewPlatform(p platform.Platform, l Layout) Platform { return Platform{Platform: p, l: l} }

func (d Platform) HostctrlExeDir() string  { return d.l.HostctrlDir }
func (d Platform) SapservicesPath() string { return d.l.Sapservices }
func (d Platform) IsPrivileged() bool      { return false }
func (d Platform) OSVersion(ctx context.Context, r exec.Runner) string {
	return d.Platform.OSVersion(ctx, r) + " · DEMO"
}
