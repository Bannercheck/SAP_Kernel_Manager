package kernel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareDirs(t *testing.T) {
	root := t.TempDir()
	central, local := filepath.Join(root, "central"), filepath.Join(root, "local")
	os.MkdirAll(central, 0o755)
	os.MkdirAll(local, 0o755)
	for _, n := range []string{"sapstartsrv", "sapcontrol", "disp+work"} {
		os.WriteFile(filepath.Join(central, n), []byte("new "+n), 0o755)
	}
	os.WriteFile(filepath.Join(local, "sapstartsrv"), []byte("new sapstartsrv"), 0o755)
	os.WriteFile(filepath.Join(local, "sapcontrol"), []byte("OLD sapcontrol"), 0o755) // stale
	s := CompareDirs(central, local)
	if s.InSync() || s.Checked != 2 || len(s.Differs) != 1 || s.Differs[0] != "sapcontrol" || len(s.Missing) != 1 || s.Missing[0] != "disp+work" {
		t.Errorf("sync = %+v", s)
	}
	os.WriteFile(filepath.Join(local, "sapcontrol"), []byte("new sapcontrol"), 0o755)
	if s := CompareDirs(central, local); !s.InSync() {
		t.Errorf("after refresh: %+v", s)
	}
	if s := CompareDirs(central, central); s.Checked != 0 || s.InSync() {
		t.Errorf("same directory must not be compared: %+v", s)
	}
}
