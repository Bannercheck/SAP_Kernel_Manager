package hub

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// Target is a host with its secrets opened, ready to dial.
type Target struct {
	Host       Host
	Password   string // Auth=password
	Passphrase string // Auth=key, may be empty
}

// Remote opens connections to SAP hosts (SSH, or the simulated demo landscape).
type Remote interface {
	Dial(ctx context.Context, t Target) (Conn, error)
}

// Conn is one connection to a host.
type Conn interface {
	// Run executes cmd through the remote login shell; stdin may be nil, stdout and stderr the same writer.
	Run(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (exit int, err error)
	// Upload streams r into path (written as path.part, then renamed) with mode.
	Upload(ctx context.Context, r io.Reader, path string, mode os.FileMode) error
	Fingerprint() string // SHA256:… of the host key
	NewKey() bool        // the host key was trusted on this connection (first contact)
	Close() error
}

// shCmd builds a command that every login shell (sh, ksh, bash, csh) passes
// unchanged to /bin/sh: the script and its arguments are single-quoted, the
// script reads them as $1, $2 … Scripts must be one line.
func shCmd(script string, args ...string) string {
	parts := []string{"/bin/sh", "-c", exec.ShellQuote(script), "km"}
	for _, a := range args {
		parts = append(parts, exec.ShellQuote(a))
	}
	return strings.Join(parts, " ")
}

// execCmd runs argv as is (exec "$@"), quoting every argument.
func execCmd(argv ...string) string { return shCmd(`exec "$@"`, argv...) }

// uploadScript writes stdin to $1 through a .part file and sets mode $2.
const uploadScript = `umask 022; mkdir -p "$(dirname "$1")" && cat > "$1.part" && chmod "$2" "$1.part" && mv -f "$1.part" "$1"`

func uploadCmd(path string, mode os.FileMode) string {
	return shCmd(uploadScript, path, fmt.Sprintf("%o", mode.Perm()))
}

// output runs cmd and returns stdout; a non-zero exit becomes an error with stderr.
func output(ctx context.Context, c Conn, cmd string) (string, error) {
	var out, errb bytes.Buffer
	code, err := c.Run(ctx, cmd, nil, &out, &errb)
	if err != nil {
		return out.String(), err
	}
	if code != 0 {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return out.String(), fmt.Errorf("exit %d: %s", code, lastLine(msg))
	}
	return out.String(), nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// lockedWriter serialises writes; it deliberately has no ReadFrom, so
// io.Copy goes through Write.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// lineWriter cuts a byte stream into lines and hands each to emit. It is
// safe for the two goroutines that copy stdout and stderr.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if strings.TrimSpace(line) != "" {
			w.emit(line)
		}
	}
	if len(w.buf) > 64<<10 { // a very long line without newline
		w.emit(string(w.buf))
		w.buf = nil
	}
	return len(p), nil
}

// Flush emits a trailing partial line.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(string(w.buf)) != "" {
		w.emit(string(w.buf))
	}
	w.buf = nil
}
