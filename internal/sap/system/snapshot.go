package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
)

// Snapshot is what KernelMan remembers about a system between runs. It is
// written whenever the system is resolved, so that a stopped sapstartsrv
// (which makes sapcontrol ParameterValue unavailable) does not block
// backup, update or rollback.
type Snapshot struct {
	SID             string               `json:"sid"`
	KernelDir       string               `json:"kernel_dir"`
	KernelDirs      []string             `json:"kernel_dirs,omitempty"`
	DirExeRoot      string               `json:"dir_exe_root,omitempty"`
	Instances       []discovery.Instance `json:"instances"`
	TakenAt         time.Time            `json:"taken_at"`
	LastBackup      string               `json:"last_backup,omitempty"`  // backup of the central kernel directory
	LastBackups     map[string]string    `json:"last_backups,omitempty"` // kernel dir → its backup
	LastBackupAt    time.Time            `json:"last_backup_at,omitempty"`
	LastDownloadDir string               `json:"last_download_dir,omitempty"`
	CopiedSARs      []string             `json:"copied_sars,omitempty"` // file names placed in KernelDir by Kernel Files
	LastUpdateAt    time.Time            `json:"last_update_at,omitempty"`
}

const snapshotFile = "snapshot.json"

// LoadSnapshot reads the snapshot in dir; a missing file is not an error.
func LoadSnapshot(dir string) (*Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(dir, snapshotFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveSnapshot applies update to the current snapshot and writes it.
func (t *Target) SaveSnapshot(update func(*Snapshot)) error {
	if t.Snapshot == nil {
		t.Snapshot = &Snapshot{SID: t.SID}
	}
	update(t.Snapshot)
	b, err := json.MarshalIndent(t.Snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(t.StateDir, snapshotFile), b, 0o644)
}
