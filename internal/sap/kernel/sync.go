package kernel

import (
	"bytes"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
)

// SyncMarkers are the executables compared between the central kernel
// directory and an instance's local exe directory.
var SyncMarkers = []string{"sapstartsrv", "sapcontrol", "disp+work", "msg_server", "enserver", "enq_server", "gwrd", "icman", "jstart", "sapcpe", "R3trans"}

// Sync is the comparison of one local exe directory with the central kernel.
type Sync struct {
	Dir     string   // the local directory
	Checked int      // markers present in both directories
	Differs []string // markers whose content differs: the instance runs another kernel level
	Missing []string // markers present centrally but not locally (informational)
}

// InSync means every marker found in both directories is identical.
func (s Sync) InSync() bool { return s.Checked > 0 && len(s.Differs) == 0 }

// CompareDirs checks whether local holds the same kernel as central by
// comparing the marker executables byte for byte (SHA-256). Checked stays 0
// when the directories are the same place or share no marker.
func CompareDirs(central, local string) Sync {
	s := Sync{Dir: local}
	c, _ := filepath.EvalSymlinks(central)
	l, _ := filepath.EvalSymlinks(local)
	if c != "" && c == l {
		return s
	}
	for _, m := range SyncMarkers {
		cs, cerr := fileSum(filepath.Join(central, m))
		if cerr != nil {
			continue
		}
		ls, lerr := fileSum(filepath.Join(local, m))
		if lerr != nil {
			s.Missing = append(s.Missing, m)
			continue
		}
		s.Checked++
		if !bytes.Equal(cs, ls) {
			s.Differs = append(s.Differs, m)
		}
	}
	return s
}

func fileSum(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
