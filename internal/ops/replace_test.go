package ops

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// denyWrite makes writeCheck refuse the named files, as for <sid>adm on a root-owned setuid file.
func denyWrite(t *testing.T, names ...string) {
	t.Helper()
	prev := writeCheck
	writeCheck = func(p string) error {
		for _, n := range names {
			if filepath.Base(p) == n {
				return fs.ErrPermission
			}
		}
		return prev(p)
	}
	t.Cleanup(func() { writeCheck = prev })
}

// A file the archive brings and this user cannot overwrite is renamed aside
// before anything else happens, so SAPCAR never stops half way.
func TestExtractMovesAsideUnwritable(t *testing.T) {
	e, fake, rec := newEnv(t)
	e.User = "abcadm"
	icmbnd := filepath.Join(e.T.KernelDir, "icmbnd")
	if err := os.WriteFile(icmbnd, []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	denyWrite(t, "icmbnd")
	sapcar := filepath.Join(e.T.KernelDir, "SAPCAR")
	fake.On(sapcar+" -tvf /dl/SAPEXE_403-1.SAR", "SAPCAR: processing archive (version 2.01)\n-rwxr-xr-x 1000 14 Oct 2026 21:33 disp+work\n-rwxr-x--- 10 14 Oct 2026 21:33 icmbnd\n", 0)
	fake.On(sapcar+" -xvf /dl/SAPEXE_403-1.SAR", "x disp+work\nx icmbnd\n", 0)
	files := []SARFile{{Name: "SAPEXE_403-1.SAR", Path: "/dl/SAPEXE_403-1.SAR", Component: "SAPEXE", Patch: 403, Full: true}}
	res, err := Extract(context.Background(), e, sapcar, files)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	aside := icmbnd + ".kernelman_20260927_100000"
	if _, err := os.Stat(aside); err != nil || len(res.Moved) != 1 || res.Moved[0] != aside {
		t.Errorf("icmbnd not moved aside: %v %v", res.Moved, err)
	}
	if !strings.Contains(rec.String(), "[1/10] Move aside 1 file(s) abcadm cannot overwrite (icmbnd)") || !strings.Contains(rec.String(), "[2/10] SAPCAR -xvf") {
		t.Errorf("progress:\n%s", rec)
	}
}

// When the archive cannot be listed and a file is not writable, nothing is touched.
func TestExtractRefusesUnknownContents(t *testing.T) {
	e, fake, rec := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.T.KernelDir, "icmbnd"), []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	denyWrite(t, "icmbnd")
	sapcar := filepath.Join(e.T.KernelDir, "SAPCAR")
	files := []SARFile{{Name: "SAPEXE_403-1.SAR", Path: "/dl/SAPEXE_403-1.SAR", Component: "SAPEXE", Patch: 403, Full: true}}
	_, err := Extract(context.Background(), e, sapcar, files)
	if err == nil || !strings.Contains(err.Error(), "cannot be overwritten (icmbnd)") || !strings.Contains(err.Error(), "run KernelMan as root") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range fake.Calls {
		if (filepath.Base(c.Path) == "SAPCAR" && c.Args[0] == "-xvf") || strings.Contains(c.String(), "StopService") {
			t.Errorf("changed something: %s\n%s", c, rec)
		}
	}
}

// Rollback as <sid>adm: cp cannot overwrite a root-owned file either.
func TestRestoreMovesAsideUnwritable(t *testing.T) {
	e, _, rec := newEnv(t)
	bk, err := Backup(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	backups := map[string]string{}
	for _, d := range bk.Dirs {
		backups[d.Source] = d.Dest
	}
	denyWrite(t, "gwrd")
	res, err := Restore(context.Background(), e, backups)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	if len(res.Moved) != 1 || !strings.HasPrefix(res.Moved[0], filepath.Join(e.T.KernelDir, "gwrd.kernelman_")) {
		t.Errorf("moved = %v\n%s", res.Moved, rec)
	}
	if _, err := os.Stat(filepath.Join(e.T.KernelDir, "gwrd")); err != nil {
		t.Errorf("gwrd not restored: %v", err)
	}
}
