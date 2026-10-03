package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMainRunsTheCommandLineAndPassesItsExitCodeOn(t *testing.T) {
	out, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	oldArgs, oldStdout, oldExit := os.Args, os.Stdout, exit
	t.Cleanup(func() { os.Args, os.Stdout, exit = oldArgs, oldStdout, oldExit })
	os.Args = []string{"meaninggraph", "version"}
	os.Stdout = out
	code := -1
	exit = func(c int) { code = c }
	main()
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	info, err := out.Stat()
	if err != nil || info.Size() == 0 {
		t.Fatalf("version printed nothing: %v", err)
	}
}
