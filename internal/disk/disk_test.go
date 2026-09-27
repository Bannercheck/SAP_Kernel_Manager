package disk

import "testing"

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
