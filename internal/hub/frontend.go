package hub

import (
	"errors"
	"strings"
	"time"
)

// The Go API the front ends share: the web handlers and the console.

// Errors a front end shows as they are.
var (
	ErrNotSetUp      = errors.New("set up the hub first")
	ErrAlreadySetUp  = errors.New("the hub is already set up")
	ErrWrongLogin    = errors.New("wrong user or password")
	ErrLoginsPaused  = errors.New("too many failed logins, wait a minute")
	ErrNeedsPassword = errors.New("enter the SSH password")
)

// Setup creates the one admin account and opens the vault. It returns the
// vault key for the session that set it up.
func (h *Hub) Setup(user, password string) error {
	_, err := h.setup(user, password)
	return err
}

func (h *Hub) setup(user, password string) ([]byte, error) {
	if h.Store.Auth() != nil {
		return nil, ErrAlreadySetUp
	}
	a, err := NewAuth(strings.TrimSpace(user), password)
	if err != nil {
		return nil, err
	}
	key, err := a.Key(password)
	if err != nil {
		return nil, err
	}
	if err := h.Store.SetAuth(a, nil); err != nil {
		return nil, err
	}
	h.sess.mu.Lock()
	h.sess.key = key
	h.sess.mu.Unlock()
	return key, nil
}

// Login checks the admin account and opens the vault; five failures in a
// row pause logins for a minute.
func (h *Hub) Login(user, password string) error {
	_, err := h.login(user, password)
	return err
}

func (h *Hub) login(user, password string) ([]byte, error) {
	a := h.Store.Auth()
	if a == nil {
		return nil, ErrNotSetUp
	}
	if h.sess.locked() {
		return nil, ErrLoginsPaused
	}
	key, ok := a.Verify(strings.TrimSpace(user), password)
	if !ok {
		h.sess.failed()
		time.Sleep(400 * time.Millisecond)
		return nil, ErrWrongLogin
	}
	h.sess.mu.Lock()
	h.sess.key, h.sess.failures = key, 0
	h.sess.mu.Unlock()
	return key, nil
}

// HostViews lists the inventory with each host's last state, without secrets.
func (h *Hub) HostViews() []HostView { return h.hostViews() }

// SaveHost adds or changes a host; secret (password or key passphrase) is
// sealed with the vault key, empty keeps the stored one.
func (h *Hub) SaveHost(host Host, secret string) (Host, error) {
	if secret != "" {
		sealed, err := Seal(h.sess.vaultKey(), secret)
		if err != nil {
			return host, err
		}
		host.Secret = sealed
	}
	if host.ID == "" && host.Auth != "key" && host.Secret == "" {
		return host, ErrNeedsPassword
	}
	return h.Store.PutHost(host)
}

// RemoveHost deletes a host from the inventory and forgets its state.
func (h *Hub) RemoveHost(id string) error {
	if err := h.Store.DeleteHost(id); err != nil {
		return err
	}
	h.forget(id)
	return nil
}
