package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Unattended (--yes) never mixes archives found in several directories.
func TestFilesYesRefusesMixedDirectories(t *testing.T) {
	fe := newFlow(t)
	fe.install(t)
	other := filepath.Join(fe.root, "tmp", "other-system")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "SAPEXE_500-80009999.SAR"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var code int
	out := captureStdout(t, func() { code = FilesOp([]string{"--yes"}) })
	if code == ExitOK || !strings.Contains(out, "today's archives are in 2 directories") || !strings.Contains(out, "--from DIR") {
		t.Errorf("code %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(fe.kernelDir, "SAPEXE_403-80007807.SAR")); err == nil {
		t.Error("archives copied although the source was ambiguous")
	}
	// one directory named with --from: fine
	out = captureStdout(t, func() { code = FilesOp([]string{"--yes", "--from", fe.download}) })
	if code != ExitOK || !strings.Contains(out, "4 archive(s) copied into 3 kernel directories") {
		t.Errorf("--from: code %d\n%s", code, out)
	}
}

// captureStdout runs fn with the CLI output going to a buffer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	var out strings.Builder
	prev := stdout
	stdout = &out
	defer func() { stdout = prev }()
	fn()
	return out.String()
}
