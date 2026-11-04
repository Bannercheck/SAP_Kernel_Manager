package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// As root, Kernel File Transfer chowns only the archives it copied: a
// chown -R of the kernel directory would strip saproot.sh's root owner and
// setuid bit from icmbnd.
func TestCopySARsChownsOnlyArchives(t *testing.T) {
	e, fake, rec := newEnv(t)
	e.IsRoot = true
	fakeBins["chown"] = true
	defer delete(fakeBins, "chown")
	dl := t.TempDir()
	src := filepath.Join(dl, "SAPEXE_403-1.SAR")
	if err := os.WriteFile(src, []byte("sar"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range e.T.KernelDirs {
		fake.On("chown abcadm:sapsys "+filepath.Join(d, "SAPEXE_403-1.SAR"), "", 0)
	}
	if _, err := CopySARs(context.Background(), e, []SARFile{{Name: "SAPEXE_403-1.SAR", Path: src, Component: "SAPEXE", Patch: 403}}); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	for _, c := range fake.Calls {
		if c.Path == "chown" && c.Args[0] == "-R" {
			t.Errorf("recursive chown: %s", c)
		}
	}
	if !strings.Contains(rec.String(), "[3/8] chown abcadm:sapsys 1 archive(s) in "+e.T.KernelDir) {
		t.Errorf("progress:\n%s", rec)
	}
}
