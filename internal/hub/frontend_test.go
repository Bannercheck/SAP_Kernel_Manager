package hub

import (
	"strings"
	"testing"
)

// The console uses the same Go API as the web handlers: login opens the
// vault, a saved host secret is sealed, a wrong password is refused.
func TestFrontendAPI(t *testing.T) {
	h, err := New(Config{DataDir: t.TempDir(), Remote: NewDemoRemote(), Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Login("admin", "whatever-123"); err == nil || !strings.Contains(err.Error(), "set up") {
		t.Errorf("login before setup: %v", err)
	}
	if err := h.Setup("admin", "short"); err == nil {
		t.Error("short password accepted")
	}
	if err := h.Setup("admin", "Secret-Admin-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.Login("admin", "wrong-password"); err == nil || !strings.Contains(err.Error(), "wrong user or password") {
		t.Errorf("wrong password: %v", err)
	}
	if err := h.Login("admin", "Secret-Admin-1"); err != nil {
		t.Fatal(err)
	}
	saved, err := h.SaveHost(Host{Name: "sapdev01", User: "tayfur", Become: "root", Elevate: "dzdo"}, "Ssh-Pass-1")
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := h.Store.Host(saved.ID)
	if stored.Secret == "" || strings.Contains(stored.Secret, "Ssh-Pass-1") {
		t.Errorf("secret not sealed: %q", stored.Secret)
	}
	if tg, err := h.target(stored); err != nil || tg.Password != "Ssh-Pass-1" {
		t.Errorf("unseal: %v", err)
	}
	if _, err := h.SaveHost(Host{Name: "sapdev02", User: "x"}, ""); err == nil {
		t.Error("new password host without a password accepted")
	}
	if len(h.HostViews()) != 1 {
		t.Errorf("views %v", h.HostViews())
	}
	if err := h.RemoveHost(saved.ID); err != nil || len(h.HostViews()) != 0 {
		t.Errorf("remove: %v", err)
	}
}
