package kernel

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseDispWork(t *testing.T) {
	v, err := Parse(load(t, "linux/dispwork_V.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := Version{Release: 793, Patch: 200, MakeVariant: "793_REL", Platform: "linuxx86_64",
		CompiledFor: "64 BIT", CompilationMode: "UNICODE", CompileTime: "Sep 14 2026 21:33:27",
		DBClient: "SQLDBC 2.20.14.1706784436", DBSL: "793.02", SourceID: "0.200"}
	if v.Release != want.Release || v.Patch != want.Patch || v.MakeVariant != want.MakeVariant ||
		v.Platform != want.Platform || v.CompiledFor != want.CompiledFor || v.CompilationMode != want.CompilationMode ||
		v.CompileTime != want.CompileTime || v.DBClient != want.DBClient || v.DBSL != want.DBSL || v.SourceID != want.SourceID {
		t.Errorf("got %+v\nwant %+v", v, want)
	}
	if got := len(v.SupportedBasis); got != 4 || v.SupportedBasis[0] != "755" || v.SupportedBasis[3] != "758" {
		t.Errorf("SupportedBasis = %v", v.SupportedBasis)
	}
	if v.String() != "793 patch 200" || v.ReleaseDotted() != "7.93" {
		t.Errorf("String()=%q ReleaseDotted()=%q", v.String(), v.ReleaseDotted())
	}
}

func TestParseHostAgent(t *testing.T) {
	v, err := Parse(load(t, "linux/saphostexec_version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Release != 722 || v.Patch != 65 || v.Platform != "linuxx86_64" {
		t.Errorf("got %+v", v)
	}
}

func TestParseNoRelease(t *testing.T) {
	if _, err := Parse("garbage\nmore garbage"); err == nil {
		t.Error("expected error for output without kernel release")
	}
}
