package hub

import (
	"strings"
	"testing"
)

// Two hub processes on one data directory would run two jobs at once and
// overwrite each other's inventory: the second one is refused.
func TestLockDataDir(t *testing.T) {
	dir := t.TempDir()
	release, err := LockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockDataDir(dir); err == nil || !strings.Contains(err.Error(), "already uses") {
		t.Fatalf("second lock: %v", err)
	}
	release()
	again, err := LockDataDir(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again()
}
