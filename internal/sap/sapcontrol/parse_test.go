package sapcontrol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "linux", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newClient(f *exec.Fake) *Client {
	return &Client{Runner: f, Path: "sapcontrol", Nr: "00"}
}

func TestGetProcessList(t *testing.T) {
	f := exec.NewFake().On("sapcontrol -nr 00 -function GetProcessList", load(t, "GetProcessList.txt"), 3)
	procs, err := newClient(f).GetProcessList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 4 {
		t.Fatalf("got %d processes", len(procs))
	}
	dw := procs[0]
	if dw.Name != "disp+work" || dw.DispStatus != "GREEN" || dw.PID != 12345 ||
		dw.TextStatus != "Running, Message Server connection ok, Dialog Queue time: 0.00 sec" ||
		dw.StartTime != "2026 09 10 08:00:05" || dw.ElapsedTime != "275:30:00" {
		t.Errorf("disp+work row parsed wrong: %+v", dw)
	}
	if procs[2].Name != "gwrd" || procs[2].TextStatus != "Running" || procs[2].PID != 12347 {
		t.Errorf("gwrd row parsed wrong: %+v", procs[2])
	}
}

func TestGetProcessListConnRefused(t *testing.T) {
	f := exec.NewFake().On("sapcontrol -nr 00 -function GetProcessList", load(t, "GetProcessList_FAIL.txt"), 1)
	_, err := newClient(f).GetProcessList(context.Background())
	if err == nil || !IsConnRefused(err) {
		t.Fatalf("expected connection refused error, got %v", err)
	}
}

func TestGetSystemInstanceList(t *testing.T) {
	f := exec.NewFake().On("sapcontrol -nr 00 -function GetSystemInstanceList", load(t, "GetSystemInstanceList.txt"), 0)
	list, err := newClient(f).GetSystemInstanceList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Nr != "01" || list[1].Nr != "00" || list[2].Hostname != "sapapp2" ||
		list[2].DispStatus != "GRAY" || len(list[1].Features) != 4 || list[1].Features[0] != "ABAP" || list[0].HTTPPort != 50113 {
		t.Errorf("got %+v", list)
	}
}

func TestGetVersionInfo(t *testing.T) {
	f := exec.NewFake().On("sapcontrol -nr 00 -function GetVersionInfo", load(t, "GetVersionInfo.txt"), 0)
	rows, err := newClient(f).GetVersionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	r := rows[1]
	if r.Filename != "/usr/sap/ABC/D00/exe/disp+work" || r.Release != 793 || r.Patch != 200 ||
		r.Changelist != 2123456 || r.Platform != "linuxx86_64" || r.Time != "2026 09 14 21:33:27" {
		t.Errorf("got %+v", r)
	}
}

func TestParameterValueAndProperties(t *testing.T) {
	f := exec.NewFake().
		On("sapcontrol -nr 00 -function ParameterValue DIR_CT_RUN", "\n20.09.2026 19:30:04\nParameterValue\nOK\n/usr/sap/ABC/SYS/exe/uc/linuxx86_64\n", 0).
		On("sapcontrol -nr 00 -function GetInstanceProperties", load(t, "GetInstanceProperties.txt"), 0)
	c := newClient(f)
	v, err := c.ParameterValue(context.Background(), "DIR_CT_RUN")
	if err != nil || v != "/usr/sap/ABC/SYS/exe/uc/linuxx86_64" {
		t.Errorf("ParameterValue = %q, %v", v, err)
	}
	props, err := c.GetInstanceProperties(context.Background())
	if err != nil || props["INSTANCE_NAME"] != "D00" || props["SAPSYSTEMNAME"] != "ABC" ||
		props["Protected Webmethods"] != "Start,Stop,Shutdown,InstanceStart" {
		t.Errorf("props = %v, %v", props, err)
	}
}

func TestAggregate(t *testing.T) {
	cases := map[string][]string{
		"GREEN": {"GREEN", "GREEN"}, "GRAY": {"GRAY"}, "YELLOW": {"GREEN", "GRAY"},
		"RED": {"GREEN", "RED", "YELLOW"},
	}
	for want, in := range cases {
		if got := Aggregate(in); got != want {
			t.Errorf("Aggregate(%v) = %s, want %s", in, got, want)
		}
	}
	if Aggregate(nil) != "GRAY" {
		t.Error("empty should be GRAY")
	}
}
