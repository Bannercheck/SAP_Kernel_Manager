package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
)

// Archive is a file in the hub's library: a kernel archive or SAPCAR that
// can be pushed to the hosts before Kernel File Transfer or Kernel Update.
type Archive struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Uploaded  time.Time `json:"uploaded"`
	Cksum     string    `json:"cksum"`  // POSIX cksum "<crc> <size>"
	SHA256    string    `json:"sha256"` // to compare with SAP's checksum
	Kind      string    `json:"kind"`   // sar | sapcar
	Component string    `json:"component,omitempty"`
	Label     string    `json:"label,omitempty"`
	Patch     int       `json:"patch,omitempty"`
	Full      bool      `json:"full,omitempty"`
}

// Library keeps uploaded files in <data>/library.
type Library struct {
	dir string
	mu  sync.Mutex
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,200}$`)

// OpenLibrary creates the library directory.
func OpenLibrary(dir string) (*Library, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Library{dir: dir}, nil
}

// checkName accepts *.SAR / *.sar and SAPCAR executables with a safe name.
func checkName(name string) (Archive, error) {
	if !safeName.MatchString(name) || strings.Contains(name, "..") {
		return Archive{}, fmt.Errorf("file name %q is not allowed", name)
	}
	if ops.IsSAPCARName(name) {
		return Archive{Name: name, Kind: "sapcar", Component: "SAPCAR", Label: "SAPCAR"}, nil
	}
	if !strings.EqualFold(filepath.Ext(name), ".sar") {
		return Archive{}, fmt.Errorf("%s: only SAP archives (*.SAR) and SAPCAR can be uploaded", name)
	}
	a := Archive{Name: name, Kind: "sar"}
	if f, ok := ops.ParseSARName(name); ok {
		a.Component, a.Label, a.Patch, a.Full = f.Component, f.Label, f.Patch, f.Full
	}
	return a, nil
}

// Save stores r as name, computing both checksums while writing.
func (l *Library) Save(name string, r io.Reader) (Archive, error) {
	a, err := checkName(name)
	if err != nil {
		return a, err
	}
	tmp, err := os.CreateTemp(l.dir, ".upload-*")
	if err != nil {
		return a, err
	}
	defer os.Remove(tmp.Name())
	var ck cksum
	sh := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, &ck, sh), r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return a, fmt.Errorf("store %s: %w", name, err)
	}
	if n == 0 {
		return a, errors.New(name + " is empty")
	}
	a.Size, a.Cksum, a.SHA256, a.Uploaded = n, ck.Sum(), hex.EncodeToString(sh.Sum(nil)), time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.Rename(tmp.Name(), filepath.Join(l.dir, name)); err != nil {
		return a, err
	}
	b, _ := json.MarshalIndent(a, "", "  ")
	return a, os.WriteFile(filepath.Join(l.dir, name+".json"), b, 0o600)
}

// List returns the archives in apply order (SAPCAR last).
func (l *Library) List() []Archive {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, _ := os.ReadDir(l.dir)
	var out []Archive
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(l.dir, e.Name()))
		var a Archive
		if err != nil || json.Unmarshal(b, &a) != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(l.dir, a.Name)); err == nil {
			out = append(out, a)
		}
	}
	rank := func(a Archive) int {
		switch {
		case a.Kind == "sapcar":
			return 3
		case a.Component == "SAPEXE":
			return 0
		case a.Component == "SAPEXEDB":
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { // the order SAPCAR applies them in, SAPCAR itself last
		if (out[i].Kind == "sapcar") != (out[j].Kind == "sapcar") {
			return out[j].Kind == "sapcar"
		}
		if out[i].Patch != out[j].Patch {
			return out[i].Patch < out[j].Patch
		}
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get returns one archive's metadata.
func (l *Library) Get(name string) (Archive, error) {
	for _, a := range l.List() {
		if a.Name == name {
			return a, nil
		}
	}
	return Archive{}, fmt.Errorf("%s is not in the library", name)
}

// Path is where an archive's bytes are.
func (l *Library) Path(name string) string { return filepath.Join(l.dir, filepath.Base(name)) }

// Delete removes an archive and its metadata.
func (l *Library) Delete(name string) error {
	if _, err := checkName(name); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.Remove(filepath.Join(l.dir, name+".json"))
	if err := os.Remove(filepath.Join(l.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// targetPatch is the kernel patch the selected archives lead to.
func targetPatch(archives []Archive) int {
	var files []ops.SARFile
	for _, a := range archives {
		if a.Kind == "sar" {
			files = append(files, ops.SARFile{Name: a.Name, Component: a.Component, Patch: a.Patch, Full: a.Full})
		}
	}
	return ops.TargetPatch(files)
}
