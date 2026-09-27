package demo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
)

func TestSimulatedHost(t *testing.T) {
	root := t.TempDir()
	l, err := Setup(root, platform.Current())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.KernelDir, "disp+work")); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(l, exec.NewReal())
	ctx := context.Background()
	c := &sapcontrol.Client{Runner: r, Path: filepath.Join(l.HostctrlDir, "sapcontrol"), Nr: "00"}

	procs, err := c.GetProcessList(ctx)
	if err != nil || len(procs) != 4 || procs[0].DispStatus != "GREEN" {
		t.Fatalf("running: %v %+v", err, procs)
	}
	if v, _ := c.ParameterValue(ctx, "DIR_CT_RUN"); v != l.KernelDir {
		t.Errorf("DIR_CT_RUN = %q", v)
	}
	if err := c.StopSystem(ctx); err != nil {
		t.Fatal(err)
	}
	procs, _ = c.GetProcessList(ctx)
	if procs[0].DispStatus != "GRAY" {
		t.Errorf("after StopSystem: %+v", procs)
	}
	if err := c.StopService(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetProcessList(ctx); !sapcontrol.IsConnRefused(err) {
		t.Errorf("after StopService expected connection refused, got %v", err)
	}
	c.RunAs = "abcadm"
	if err := c.StartService(ctx, SID); err != nil {
		t.Fatal(err)
	}
	if err := c.StartSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if procs, _ = c.GetProcessList(ctx); procs[0].DispStatus != "GREEN" {
		t.Errorf("after start: %+v", procs)
	}

	// SAPCAR raises the patch level that disp+work -V reports
	res, _ := r.Run(ctx, exec.Cmd{Path: filepath.Join(l.KernelDir, "SAPCAR"), Args: []string{"-xvf", filepath.Join(l.Download, "dw_423-80007541.sar")}})
	if !strings.Contains(res.Stdout, "x disp+work") {
		t.Errorf("SAPCAR output: %q", res.Stdout)
	}
	res, _ = r.Run(ctx, exec.Cmd{Path: filepath.Join(l.KernelDir, "disp+work"), Args: []string{"-V"}})
	if !strings.Contains(res.Stdout, "patch number                  423") {
		t.Errorf("disp+work -V after SAPCAR: %q", res.Stdout)
	}
	// real commands pass through
	res, err = r.Run(ctx, exec.Cmd{Path: "ls", Args: []string{l.Download}})
	if err != nil || !strings.Contains(res.Stdout, "SAPEXE_403-80007807.SAR") {
		t.Errorf("ls passthrough: %v %q", err, res.Stdout)
	}
	// setup is idempotent
	if _, err := Setup(root, platform.Current()); err != nil {
		t.Fatal(err)
	}
}
