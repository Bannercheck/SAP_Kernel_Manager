package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "linux", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseListInstances(t *testing.T) {
	list := ParseListInstances(load(t, "ListInstances.txt"))
	if len(list) != 3 {
		t.Fatalf("got %d instances", len(list))
	}
	if list[0].SID != "ABC" || list[0].Nr != "00" || list[0].Host != "sapci" || list[0].Release != 793 ||
		list[0].Patch != 200 || list[0].Changelist != 2123456 {
		t.Errorf("row0 = %+v", list[0])
	}
	if list[2].SID != "HDB" || list[2].Release != 753 || list[2].Patch != 1200 || list[2].Changelist != 0 {
		t.Errorf("row2 = %+v", list[2])
	}
}

func TestParseSapservices(t *testing.T) {
	list := ParseSapservices(load(t, "sapservices"))
	if len(list) != 2 {
		t.Fatalf("got %d instances: %+v", len(list), list)
	}
	ascs, d := list[0], list[1]
	if ascs.SID != "ABC" || ascs.Name != "ASCS01" || ascs.Type != "ASCS" || ascs.Nr != "01" || ascs.Host != "sapci" ||
		ascs.Profile != "/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci" || ascs.ExeDir != "/usr/sap/ABC/ASCS01/exe" {
		t.Errorf("ascs = %+v", ascs)
	}
	if d.Name != "D00" || d.Type != "D" || d.Nr != "00" || d.ExeDir != "/usr/sap/ABC/D00/exe" {
		t.Errorf("d00 = %+v", d)
	}
}

func TestParseInstanceName(t *testing.T) {
	cases := map[string][2]string{"DVEBMGS00": {"DVEBMGS", "00"}, "ASCS01": {"ASCS", "01"}, "J00": {"J", "00"}, "ERS10": {"ERS", "10"}}
	for in, want := range cases {
		typ, nr, ok := ParseInstanceName(in)
		if !ok || typ != want[0] || nr != want[1] {
			t.Errorf("ParseInstanceName(%s) = %s %s %v", in, typ, nr, ok)
		}
	}
	if _, _, ok := ParseInstanceName("profile"); ok {
		t.Error("expected failure for non-instance name")
	}
}

func TestMerge(t *testing.T) {
	a := ParseListInstances(load(t, "ListInstances.txt"))
	b := ParseSapservices(load(t, "sapservices"))
	m := merge(a, b)
	if len(m) != 3 {
		t.Fatalf("merged %d instances", len(m))
	}
	if m[0].Name != "D00" || m[0].Release != 793 || m[0].Source != "saphostctrl+sapservices" {
		t.Errorf("merged[0] = %+v", m[0])
	}
	if m[1].Name != "ASCS01" || m[2].SID != "HDB" || m[2].Name != "" {
		t.Errorf("merged = %+v", m)
	}
}
