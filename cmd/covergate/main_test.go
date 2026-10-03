package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMainRunsTheGate(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "cover.out")
	if err := os.WriteFile(profile, []byte("mode: set\na.go:1.1,2.2 1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	oldArgs, oldStdout, oldExit := os.Args, os.Stdout, exit
	t.Cleanup(func() { os.Args, os.Stdout, exit = oldArgs, oldStdout, oldExit })
	os.Args = []string{"covergate", profile}
	os.Stdout = out
	code := -1
	exit = func(c int) { code = c }
	main()
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if _, err := open(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("opening a missing profile must fail")
	}
	rc, err := open(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()
}
