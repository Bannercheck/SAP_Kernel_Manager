package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHRemote reaches hosts over SSH. Host keys are trusted on first use and
// stored in the hub; a changed key refuses the connection.
type SSHRemote struct {
	Store *Store
}

// Dial connects and authenticates.
func (r *SSHRemote) Dial(ctx context.Context, t Target) (Conn, error) {
	auth, err := authMethods(t)
	if err != nil {
		return nil, err
	}
	addr := t.Host.Addr()
	c := &sshConn{}
	cfg := &ssh.ClientConfig{
		User:    t.Host.User,
		Auth:    auth,
		Timeout: 20 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return r.checkHostKey(addr, key, c)
		},
	}
	d := net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = nc.SetDeadline(time.Now().Add(45 * time.Second))
	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("login %s@%s refused: wrong password or key (%v)", t.Host.User, addr, err)
		}
		return nil, fmt.Errorf("ssh %s: %w", addr, err)
	}
	_ = nc.SetDeadline(time.Time{})
	c.client = ssh.NewClient(cc, chans, reqs)
	c.stop = make(chan struct{})
	go c.keepalive()
	return c, nil
}

func (r *SSHRemote) checkHostKey(addr string, key ssh.PublicKey, c *sshConn) error {
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	c.fp = ssh.FingerprintSHA256(key)
	stored := r.Store.HostKey(addr)
	if stored == "" {
		c.newKey = true
		return r.Store.TrustHostKey(addr, line)
	}
	if stored == line {
		return nil
	}
	want := "unknown"
	if k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(stored)); err == nil {
		want = ssh.FingerprintSHA256(k)
	}
	return fmt.Errorf("HOST KEY CHANGED for %s: trusted %s, now %s — possible man-in-the-middle; if the server was reinstalled, delete the host in the hub and add it again", addr, want, c.fp)
}

func authMethods(t Target) ([]ssh.AuthMethod, error) {
	switch t.Host.Auth {
	case "key":
		if t.Host.KeyPath == "" {
			return nil, errors.New("no private key file set for this host")
		}
		pem, err := os.ReadFile(t.Host.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(t.Passphrase))
		}
		if err != nil {
			return nil, fmt.Errorf("private key %s: %w", t.Host.KeyPath, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		if t.Password == "" {
			return nil, errors.New("no password stored for this host")
		}
		pw := t.Password
		// AIX and PAM setups often ask through keyboard-interactive instead of password
		ki := ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = pw
			}
			return answers, nil
		})
		return []ssh.AuthMethod{ssh.Password(pw), ki}, nil
	}
}

type sshConn struct {
	client *ssh.Client
	fp     string
	newKey bool
	stop   chan struct{}
	once   sync.Once
}

func (c *sshConn) Fingerprint() string { return c.fp }
func (c *sshConn) NewKey() bool        { return c.newKey }

// keepalive keeps long kernel updates alive through firewalls and NAT.
func (c *sshConn) keepalive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			if _, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return
			}
		}
	}
}

func (c *sshConn) Close() error {
	c.once.Do(func() { close(c.stop) })
	return c.client.Close()
}

func (c *sshConn) Run(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return c.run(ctx, cmd, stdin, stdout, stderr)
}

func (c *sshConn) run(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	s, err := c.client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("open session: %w", err)
	}
	defer s.Close()
	if stdout != nil && stdout == stderr {
		// two goroutines copy stdout and stderr; one shared writer must be serialised
		lw := &lockedWriter{w: stdout}
		stdout, stderr = lw, lw
	}
	s.Stdin, s.Stdout, s.Stderr = stdin, stdout, stderr
	if err := s.Start(cmd); err != nil {
		return -1, fmt.Errorf("start command: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		var ee *ssh.ExitError
		var missing *ssh.ExitMissingError
		switch {
		case err == nil:
			return 0, nil
		case errors.As(err, &ee):
			return ee.ExitStatus(), nil
		case errors.As(err, &missing):
			return -1, errors.New("connection closed before the command finished")
		default:
			return -1, err
		}
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGTERM)
		_ = s.Close()
		return -1, ctx.Err()
	}
}

func (c *sshConn) Upload(ctx context.Context, r io.Reader, path string, mode os.FileMode) error {
	var errb strings.Builder
	code, err := c.run(ctx, uploadCmd(path, mode), r, io.Discard, &errb)
	if err != nil {
		return fmt.Errorf("upload %s: %w", path, err)
	}
	if code != 0 {
		return fmt.Errorf("upload %s: exit %d: %s", path, code, lastLine(errb.String()))
	}
	return nil
}
