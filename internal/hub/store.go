// Package hub is KernelMan Hub: one server (Windows or Linux) that keeps an
// inventory of SAP hosts, reaches them over SSH, deploys the kernelman agent
// and runs its operations, and shows everything on a web dashboard.
package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Host is one SAP server the hub manages.
type Host struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"` // hostname or IP the hub connects to
	Port    int       `json:"port"`
	User    string    `json:"user"`
	Auth    string    `json:"auth"`               // password | key
	KeyPath string    `json:"key_path,omitempty"` // private key on the hub machine
	Secret  string    `json:"secret,omitempty"`   // password or key passphrase, sealed with the admin key
	Become  string    `json:"become,omitempty"`   // run operations as: "" the login user, "root" or "sidadm" through sudo
	SudoPW  bool      `json:"sudo_pw,omitempty"`  // sudo asks for a password: give it the login password (sudo -S)
	Elevate string    `json:"elevate,omitempty"`  // how: "" sudo, "dzdo" (Centrify), "sudo-su" / "dzdo-su" (… su - <user> -c)
	Label   string    `json:"label,omitempty"`    // free text shown next to the name
	Added   time.Time `json:"added"`
}

// Addr is host:port.
func (h Host) Addr() string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", h.Name, port)
}

// Settings are the hub-wide options.
type Settings struct {
	RemoteDir string               `json:"remote_dir"`         // where archives pushed from the hub land on the hosts
	Unlocked  map[string]time.Time `json:"unlocked,omitempty"` // SID → changes allowed until; every other system is read-only
}

// UnlockFor is how long a system stays open for changes.
const UnlockFor = 8 * time.Hour

// AuthRecord is the admin account: a PBKDF2 hash for the login and a second
// salt from which the key that seals host secrets is derived.
type AuthRecord struct {
	User    string `json:"user"`
	Salt    []byte `json:"salt"`
	Hash    []byte `json:"hash"`
	KeySalt []byte `json:"key_salt"`
	Iter    int    `json:"iter"`
}

type storeData struct {
	Hosts    []Host            `json:"hosts"`
	HostKeys map[string]string `json:"host_keys"` // host:port → authorized_keys line (trust on first use)
	Settings Settings          `json:"settings"`
	Auth     *AuthRecord       `json:"auth,omitempty"`
}

// Store persists the inventory in <dir>/hub.json (mode 0600).
type Store struct {
	dir string
	mu  sync.Mutex
	d   storeData
}

// hostRe and userRe allow DNS names, IPv4/IPv6 addresses and Unix or
// domain (user@domain) logins — nothing a shell would interpret.
var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`)
	userRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}(@[A-Za-z0-9][A-Za-z0-9.-]{0,252})?$`)
)

// DefaultRemoteDir is where archives pushed from the hub are placed.
const DefaultRemoteDir = "/usr/sap/download/kernelman"

// DefaultDataDir is %AppData%\KernelManHub on Windows, ~/.config/KernelManHub elsewhere.
func DefaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return filepath.Join(d, "KernelManHub")
	}
	return ".kernelman-hub"
}

// OpenStore loads (or creates) the store in dir.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("data directory %s: %w", dir, err)
	}
	s := &Store{dir: dir, d: storeData{HostKeys: map[string]string{}, Settings: Settings{RemoteDir: DefaultRemoteDir}}}
	b, err := os.ReadFile(s.path())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, s.save()
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", s.path(), err)
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path(), err)
	}
	if s.d.HostKeys == nil {
		s.d.HostKeys = map[string]string{}
	}
	if s.d.Settings.RemoteDir == "" {
		s.d.Settings.RemoteDir = DefaultRemoteDir
	}
	return s, nil
}

// Dir is the data directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path() string { return filepath.Join(s.dir, "hub.json") }

// save writes atomically; the caller holds mu (or is the constructor).
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// Hosts returns the inventory sorted by name.
func (s *Store) Hosts() []Host {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Host(nil), s.d.Hosts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Host returns one host by ID.
func (s *Store) Host(id string) (Host, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.d.Hosts {
		if h.ID == id {
			return h, true
		}
	}
	return Host{}, false
}

// PutHost adds a host (empty ID) or replaces the one with the same ID.
func (s *Store) PutHost(h Host) (Host, error) {
	h.Name = strings.TrimSpace(h.Name)
	h.User = strings.TrimSpace(h.User)
	if h.Name == "" || h.User == "" {
		return h, errors.New("host name and user are required")
	}
	if !hostRe.MatchString(h.Name) {
		return h, fmt.Errorf("host name %q: use a DNS name or an IP address", h.Name)
	}
	if !userRe.MatchString(h.User) {
		return h, fmt.Errorf("user %q: letters, digits, . _ - and user@domain only", h.User)
	}
	if h.Port == 0 {
		h.Port = 22
	}
	if h.Auth != "key" {
		h.Auth = "password"
	}
	switch h.Become {
	case "", "root", "sidadm":
	default:
		return h, fmt.Errorf("unknown run-as mode %q", h.Become)
	}
	if h.SudoPW && (h.Become == "" || h.Auth != "password") {
		return h, errors.New("a sudo password needs a password login and a sudo run-as mode")
	}
	switch h.Elevate {
	case "", "dzdo", "sudo-su", "dzdo-su":
	default:
		return h, fmt.Errorf("unknown elevation %q (sudo, dzdo, sudo-su, dzdo-su)", h.Elevate)
	}
	if h.Become == "" {
		h.Elevate = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.d.Hosts {
		if h.ID != "" && old.ID == h.ID {
			if h.Secret == "" { // unchanged secret when the form left it empty
				h.Secret = old.Secret
			}
			h.Added = old.Added
			s.d.Hosts[i] = h
			return h, s.save()
		}
		if h.ID == "" && strings.EqualFold(old.Name, h.Name) && old.Port == h.Port {
			return h, fmt.Errorf("host %s is already in the inventory", h.Addr())
		}
	}
	if h.ID == "" {
		h.ID = newID(4)
		h.Added = time.Now()
	}
	s.d.Hosts = append(s.d.Hosts, h)
	return h, s.save()
}

// DeleteHost removes a host and forgets its host key.
func (s *Store) DeleteHost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, h := range s.d.Hosts {
		if h.ID == id {
			s.d.Hosts = append(s.d.Hosts[:i], s.d.Hosts[i+1:]...)
			delete(s.d.HostKeys, h.Addr())
			return s.save()
		}
	}
	return fmt.Errorf("host %s not found", id)
}

// HostKey returns the trusted key line for addr.
func (s *Store) HostKey(addr string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.HostKeys[addr]
}

// TrustHostKey records the key line for addr (first connection).
func (s *Store) TrustHostKey(addr, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.HostKeys[addr] = line
	return s.save()
}

// Settings returns the hub settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Settings
}

// PutSettings validates and stores the settings.
func (s *Store) PutSettings(st Settings) error {
	st.RemoteDir = strings.TrimRight(strings.TrimSpace(st.RemoteDir), "/")
	if !strings.HasPrefix(st.RemoteDir, "/") || strings.ContainsAny(st.RemoteDir, " '\"$`\\;&|") {
		return errors.New("remote directory must be an absolute path without spaces or shell characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st.Unlocked = s.d.Settings.Unlocked // locks change only through SetLock
	s.d.Settings = st
	return s.save()
}

// Unlocked reports until when changes on sid are allowed.
func (s *Store) Unlocked(sid string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.d.Settings.Unlocked[strings.ToUpper(sid)]
	return until, ok && time.Now().Before(until)
}

// SetLock opens sid for changes for UnlockFor, or closes it again.
func (s *Store) SetLock(sid string, unlock bool) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Settings.Unlocked == nil {
		s.d.Settings.Unlocked = map[string]time.Time{}
	}
	sid = strings.ToUpper(sid)
	for k, until := range s.d.Settings.Unlocked { // forget expired unlocks
		if time.Now().After(until) {
			delete(s.d.Settings.Unlocked, k)
		}
	}
	var until time.Time
	if unlock {
		until = time.Now().Add(UnlockFor)
		s.d.Settings.Unlocked[sid] = until
	} else {
		delete(s.d.Settings.Unlocked, sid)
	}
	return until, s.save()
}

// Auth returns the admin record (nil before setup).
func (s *Store) Auth() *AuthRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Auth
}

// SetAuth stores the admin record and re-seals every host secret with the new key.
func (s *Store) SetAuth(a *AuthRecord, reseal func(string) (string, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reseal != nil {
		for i := range s.d.Hosts {
			if s.d.Hosts[i].Secret == "" {
				continue
			}
			v, err := reseal(s.d.Hosts[i].Secret)
			if err != nil {
				return fmt.Errorf("re-seal secret of %s: %w", s.d.Hosts[i].Name, err)
			}
			s.d.Hosts[i].Secret = v
		}
	}
	s.d.Auth = a
	return s.save()
}

func newID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
