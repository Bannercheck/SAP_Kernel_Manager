package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An instance directory behind the central kernel is not started unless the
// operator says so; unattended (no answer) it is refused.
func TestStartRefusesInstanceBehind(t *testing.T) {
	fe := newFlow(t)
	fe.sim = newSim(true, false)
	fe.install(t)
	ascs := fe.target.KernelDirs[2]
	if err := os.WriteFile(filepath.Join(ascs, "disp+work"), []byte("#!/bin/sh\n# old level\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := input
	input = bufio.NewReader(strings.NewReader("")) // no answer, as from the hub
	defer func() { input = prev }()
	var code int
	out := captureStdout(t, func() { code = StartOp(nil) })
	if code == ExitOK || !strings.Contains(out, ascs+" is not at the central kernel level (disp+work differ)") || !strings.Contains(out, "not started") {
		t.Errorf("code %d\n%s", code, out)
	}
	if strings.Contains(strings.Join(fe.sim.Calls(), ","), "StartSystem") {
		t.Errorf("started anyway: %v", fe.sim.Calls())
	}
}
