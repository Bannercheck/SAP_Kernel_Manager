// Package kernel understands SAP kernel versions: the `disp+work -V` output
// and (later) kernel archive naming and compatibility rules.
package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/platform"
)

// Version is the parsed result of `disp+work -V` (or any SAP binary's -V/-version).
type Version struct {
	Release         int      `json:"release"`              // 793
	Patch           int      `json:"patch"`                // 200
	Changelist      int      `json:"changelist,omitempty"` // 0 when not printed
	UpdateLevel     int      `json:"update_level,omitempty"`
	SourceID        string   `json:"source_id,omitempty"`
	MakeVariant     string   `json:"make_variant,omitempty"`     // 793_REL
	Platform        string   `json:"platform,omitempty"`         // linuxx86_64, rs6000_64, NTAMD64
	CompiledOn      string   `json:"compiled_on,omitempty"`      // full "compiled on" line
	CompiledFor     string   `json:"compiled_for,omitempty"`     // 64 BIT
	CompilationMode string   `json:"compilation_mode,omitempty"` // UNICODE
	CompileTime     string   `json:"compile_time,omitempty"`
	DBClient        string   `json:"db_client,omitempty"` // SQLDBC 2.20.14 ...
	DBSL            string   `json:"dbsl,omitempty"`      // 793.02
	SupportedBasis  []string `json:"supported_basis,omitempty"`
}

// String renders "793 patch 200" (plus changelist when known).
func (v Version) String() string {
	if v.Release == 0 {
		return "unknown"
	}
	s := fmt.Sprintf("%d patch %d", v.Release, v.Patch)
	if v.Changelist > 0 {
		s += fmt.Sprintf(" (changelist %d)", v.Changelist)
	}
	return s
}

// ReleaseDotted renders 793 as "7.93".
func (v Version) ReleaseDotted() string {
	if v.Release < 100 {
		return strconv.Itoa(v.Release)
	}
	return fmt.Sprintf("%d.%02d", v.Release/100, v.Release%100)
}

// keyValRe matches "key   value" lines: key, two or more spaces, value.
var keyValRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 ,()/+_-]*?)\s{2,}(\S.*)$`)

// digitsRe matches continuation lines of the SVERS block.
var digitsRe = regexp.MustCompile(`^\s*(\d+)\s*$`)

// Parse converts `disp+work -V` style output into a Version.
func Parse(out string) (Version, error) {
	var v Version
	inSvers := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, " \r\t")
		if inSvers {
			if m := digitsRe.FindStringSubmatch(line); m != nil {
				v.SupportedBasis = append(v.SupportedBasis, m[1])
				continue
			}
			inSvers = false
		}
		m := keyValRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
		switch key {
		case "kernel release":
			v.Release = atoi(val)
		case "patch number":
			v.Patch = atoi(val)
		case "changelist":
			v.Changelist = atoi(val)
		case "update level":
			v.UpdateLevel = atoi(val)
		case "source id":
			v.SourceID = val
		case "kernel make variant":
			v.MakeVariant = val
		case "compiled on":
			v.CompiledOn = val
			if i := strings.LastIndex(val, " for "); i >= 0 {
				v.Platform = strings.TrimSpace(val[i+5:])
			}
		case "compiled for":
			v.CompiledFor = val
		case "compilation mode":
			v.CompilationMode = val
		case "compile time":
			v.CompileTime = val
		case "dbms client library":
			v.DBClient = val
		case "dbsl shared library version":
			v.DBSL = val
		case "database (sap, table svers)":
			v.SupportedBasis = append(v.SupportedBasis, val)
			inSvers = true
		}
	}
	if v.Release == 0 {
		return v, fmt.Errorf("kernel: no 'kernel release' line found in output")
	}
	return v, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.Fields(s + " 0")[0])
	return n
}

// Probe runs `<dir>/disp+work -V` with the library path pointed at dir and
// parses the result. It is the authoritative way to learn a kernel's version.
func Probe(ctx context.Context, r exec.Runner, p platform.Platform, dir string) (Version, error) {
	bin := filepath.Join(dir, "disp+work"+p.ExeSuffix())
	if _, err := os.Stat(bin); err != nil {
		return Version{}, fmt.Errorf("kernel: %s: %w", bin, err)
	}
	libVar := p.LibPathVar()
	libVal := dir
	if cur := os.Getenv(libVar); cur != "" {
		libVal += string(os.PathListSeparator) + cur
	}
	res, err := r.Run(ctx, exec.Cmd{Path: bin, Args: []string{"-V"}, Env: []string{libVar + "=" + libVal}})
	if err != nil {
		return Version{}, err
	}
	v, perr := Parse(res.Stdout)
	if perr != nil {
		return v, fmt.Errorf("%s -V (exit %d): %w", bin, res.ExitCode, perr)
	}
	return v, nil
}
