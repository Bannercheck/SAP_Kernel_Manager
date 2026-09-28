package ops

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// PasswdFile lists the accounts whose home directories are searched; tests point it elsewhere.
var PasswdFile = "/etc/passwd"

// HomeDirs returns the existing home directories of every account in
// PasswdFile plus $HOME, without duplicates and without system locations.
// Kernel archives are usually uploaded into a personal home directory
// (/home/<user>, /export/home/<user>, ...). Statting each one also mounts
// automounted (autofs/NFS) homes, which a listing of /home does not show.
func HomeDirs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "" || dir == "/" || dir == "." || seen[dir] || excluded(dir, PrunePaths) || excluded(dir, systemHomes) {
			return
		}
		seen[dir] = true
		if st, err := os.Stat(dir); err == nil && st.IsDir() { // triggers the automounter
			out = append(out, dir)
		}
	}
	if f, err := os.Open(PasswdFile); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Split(sc.Text(), ":")
			if len(fields) >= 6 && strings.HasPrefix(fields[5], "/") {
				add(fields[5])
			}
		}
	}
	if h := os.Getenv("HOME"); h != "" {
		add(h)
	}
	return out
}

// systemHomes are home directories of daemon accounts: never places for kernel archives.
var systemHomes = []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/games", "/nonexistent", "/var/empty", "/var/adm", "/var/ftp"}
