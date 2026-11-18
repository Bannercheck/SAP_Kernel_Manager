package hub

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
)

// LockDataDir makes sure only one hub process (dashboard or console) uses a
// data directory: two would run two jobs at once and overwrite each other's
// inventory. It returns the function that releases the lock. A lock left by
// a hub that no longer runs is taken over.
func LockDataDir(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("data directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, "hub.lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := fmt.Fprintf(f, "%d\n", os.Getpid())
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("lock %s: %w", path, werr)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		b, _ := os.ReadFile(path)
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 && platform.ProcessAlive(pid) {
			return nil, fmt.Errorf("another KernelMan Hub (process %d) already uses %s: stop it first or start with another --data directory (no hub running? delete %s)", pid, dir, path)
		}
		_ = os.Remove(path) // left behind by a hub that is gone
	}
	return nil, fmt.Errorf("lock %s: could not take it over", path)
}
