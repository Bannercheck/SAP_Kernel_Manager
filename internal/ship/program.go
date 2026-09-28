// Package ship sends today's kernel archives together with KernelMan itself
// to other servers over ssh/scp, so the program never has to be carried
// separately.
package ship

import (
	_ "embed"
	"fmt"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed kernelman.sh
var launcher string

// Launcher is the kernelman.sh start script (also used by the Makefile
// through `go run`-free copying: the file itself lives next to this code).
func Launcher() string { return launcher }

// Program describes the KernelMan installation that will be sent along.
type Program struct {
	Dir     string   // directory holding kernelman.sh and bin/
	Files   []string // relative paths inside Dir
	Created bool     // Dir was assembled in a temporary place
}

// Locate finds the distribution directory this binary runs from
// (<dir>/kernelman.sh + <dir>/bin/kernelman-<os>-<arch>). When the binary
// runs from somewhere else, a distribution is assembled under tmp from the
// embedded launcher and the running executable. extras are files to carry
// along in bin/ when they exist, such as the SAPCAR in use (stored as
// bin/SAPCAR, so the other server finds it without any SAP directory).
func Locate(tmp string, extras ...string) (Program, error) {
	exe, err := os.Executable()
	if err != nil {
		return Program{}, err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if filepath.Base(filepath.Dir(exe)) == "bin" {
		root := filepath.Dir(filepath.Dir(exe))
		if _, err := os.Stat(filepath.Join(root, "kernelman.sh")); err == nil {
			addExtras(root, extras)
			return Program{Dir: root, Files: listFiles(root)}, nil
		}
	}
	root := filepath.Join(tmp, "kernelman")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		return Program{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "kernelman.sh"), []byte(launcher), 0o755); err != nil {
		return Program{}, err
	}
	dst := filepath.Join(root, "bin", fmt.Sprintf("kernelman-%s-%s", runtime.GOOS, runtime.GOARCH))
	if err := copyFile(exe, dst, 0o755); err != nil {
		return Program{}, err
	}
	addExtras(root, extras)
	return Program{Dir: root, Files: listFiles(root), Created: true}, nil
}

// addExtras copies each existing extra into <root>/bin unless a file of that
// name is already there; SAPCAR-named files are stored as bin/SAPCAR.
func addExtras(root string, extras []string) {
	for _, src := range extras {
		if src == "" {
			continue
		}
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := filepath.Base(src)
		if ops.IsSAPCARName(name) {
			name = "SAPCAR"
		}
		dst := filepath.Join(root, "bin", name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		_ = copyFile(src, dst, 0o755)
	}
}

func listFiles(root string) []string {
	var files []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
