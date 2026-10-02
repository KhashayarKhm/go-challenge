package envfile

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const key = "ENVFILE_TEST_VALUE"

// unset removes key for the test and restores the previous state afterwards.
// (t.Setenv cannot express "unset", and godotenv never overrides a set key.)
func unset(t *testing.T) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key)
		}
	})
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func TestLoadSetsVariables(t *testing.T) {
	unset(t)
	if err := Load(writeFile(t, key+"=from-file\n"), true); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv(key); got != "from-file" {
		t.Fatalf("%s = %q, want from-file", key, got)
	}
}

func TestLoadKeepsRealEnvironment(t *testing.T) {
	t.Setenv(key, "from-env")
	if err := Load(writeFile(t, key+"=from-file\n"), true); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv(key); got != "from-env" {
		t.Fatalf("%s = %q, want from-env", key, got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")
	if err := Load(missing, false); err != nil {
		t.Fatalf("optional missing file: %v", err)
	}
	if err := Load(missing, true); err == nil {
		t.Fatal("required missing file: expected error")
	}
}

func TestRegisterDefaultIsOptional(t *testing.T) {
	t.Chdir(t.TempDir()) // no .env here
	fs := newFlags()
	load := Register(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := load(); err != nil {
		t.Fatalf("missing default .env must be ignored: %v", err)
	}
}

func TestRegisterExplicitPath(t *testing.T) {
	unset(t)
	fs := newFlags()
	load := Register(fs)
	if err := fs.Parse([]string{"-env-file", writeFile(t, key+"=explicit\n")}); err != nil {
		t.Fatal(err)
	}
	if err := load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := os.Getenv(key); got != "explicit" {
		t.Fatalf("%s = %q, want explicit", key, got)
	}
}

func TestRegisterExplicitMissingPathFails(t *testing.T) {
	fs := newFlags()
	load := Register(fs)
	if err := fs.Parse([]string{"-env-file", filepath.Join(t.TempDir(), "typo.env")}); err != nil {
		t.Fatal(err)
	}
	if err := load(); err == nil {
		t.Fatal("expected error for an explicit path that does not exist")
	}
}
