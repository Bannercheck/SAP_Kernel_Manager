package disk

import (
	"context"
	"os"
	"testing"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

func TestParseDF(t *testing.T) {
	linux := "Filesystem     1024-blocks      Used Available Capacity Mounted on\n/dev/mapper/vg-sap   209612800 86016000 123596800      41% /usr/sap\n"
	fs, ok := ParseDF(linux)
	if !ok || fs.Mount != "/usr/sap" || fs.SizeKB != 209612800 || fs.AvailKB != 123596800 || fs.UsePct != 41 || fs.Device != "/dev/mapper/vg-sap" {
		t.Errorf("linux: %+v %v", fs, ok)
	}
	wrapped := "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/mapper/a-very-long-volume-group-name-sapmnt\n   52428800 20971520  31457280      40% /sapmnt\n"
	fs, ok = ParseDF(wrapped)
	if !ok || fs.Mount != "/sapmnt" || fs.AvailKB != 31457280 || fs.UsePct != 40 {
		t.Errorf("wrapped: %+v %v", fs, ok)
	}
	aix := "Filesystem    1024-blocks      Used Available Capacity Mounted on\n/dev/sapdatalv   104857600  52428800  52428800      50% /usr/sap\n"
	if fs, ok = ParseDF(aix); !ok || fs.UsePct != 50 {
		t.Errorf("aix: %+v %v", fs, ok)
	}
	if _, ok = ParseDF("garbage"); ok {
		t.Error("garbage parsed")
	}
}

func TestParseDU(t *testing.T) {
	sizes := ParseDU("1048576\t/usr/sap/ABC\n20480\t/usr/sap/hostctrl\n4194304\t/usr/sap/trans\ndu: cannot read directory '/usr/sap/x': Permission denied\n")
	if len(sizes) != 3 || sizes[0].Path != "/usr/sap/trans" || sizes[2].Path != "/usr/sap/hostctrl" {
		t.Errorf("sizes = %+v", sizes)
	}
	if Human(1048576) != "1.0 GB" || Human(20480) != "20.0 MB" || Human(512) != "512 KB" || Human(3<<30) != "3.0 TB" {
		t.Errorf("Human: %s %s %s %s", Human(1048576), Human(20480), Human(512), Human(3<<30))
	}
}

func TestBuildTree(t *testing.T) {
	out := "8\t/usr/sap/ABC/D00/work\n12000000\t/usr/sap/ABC/D00\n1200000\t/usr/sap/ABC/ASCS01\n8400000\t/usr/sap/ABC/SYS\n" +
		"33554432\t/usr/sap/ABC\n10485760\t/usr/sap/QAS\n1153433\t/usr/sap/trans\n40\t/usr/sap/.kernelman\n45193625\t/usr/sap\n"
	n := BuildTree("/usr/sap", ParseDU(out), 2)
	if n == nil || n.KB != 45193625 || len(n.Children) != 3 || n.Children[0].Name != "ABC" || n.Children[2].Name != "trans" {
		t.Fatalf("tree = %+v", n)
	}
	abc := n.Children[0]
	if len(abc.Children) != 3 || abc.Children[0].Name != "D00" || abc.Children[1].Name != "SYS" || len(abc.Children[0].Children) != 0 {
		t.Errorf("ABC children = %+v", abc.Children)
	}
	if BuildTree("/nope", ParseDU(out), 1) != nil {
		t.Error("unknown root must give nil")
	}
}

func TestDFSharedFilesystem(t *testing.T) {
	root := t.TempDir()
	a, b := root+"/usr/sap", root+"/sapmnt"
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out := "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vda 1000 400 600 40% /\n"
	f := exec.NewFake().On("df -Pk "+a, out, 0).On("df -Pk "+b, out, 0)
	fss, err := DF(context.Background(), f, a, b)
	if err != nil || len(fss) != 1 || !fss[0].Holds(a) || !fss[0].Holds(b) || fss[0].Holds(root) {
		t.Errorf("one file system holding both paths expected: %+v %v", fss, err)
	}
}
