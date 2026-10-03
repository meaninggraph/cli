package meaning

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOSFSReadsTheHostFileSystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	var fsys FS = OSFS{}
	data, err := fsys.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(data) != "a" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	entries, err := fsys.ReadDir(dir)
	if err != nil || len(entries) != 2 || entries[0].Name() != "a.txt" {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}
	if info, err := fsys.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("Stat = %v, %v", info, err)
	}
	if _, err := fsys.ReadFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file must fail")
	}
}

func TestSortFindingsAndHasErrors(t *testing.T) {
	t.Parallel()
	list := []Finding{
		{File: "b", Rule: "r", Severity: Error, Message: "m"},
		{File: "a", Line: 2, Rule: "r", Severity: Warning, Message: "m"},
		{File: "a", Line: 2, Rule: "r", Severity: Warning, Message: "l"},
		{File: "a", Line: 1, Rule: "z", Severity: Info, Message: "m"},
		{File: "a", Line: 2, Rule: "a", Severity: Error, Message: "m"},
	}
	SortFindings(list)
	var got []string
	for _, f := range list {
		got = append(got, f.File+f.Rule+f.Message)
	}
	if want := []string{"azm", "aam", "arl", "arm", "brm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if !HasErrors(list) || HasErrors(list[2:4]) {
		t.Fatal("HasErrors")
	}
}

func TestOSFSReadsAtMostTheBytesAsked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		max  int64
		want error
	}{{5, nil}, {100, nil}, {4, ErrTooLarge}, {0, ErrTooLarge}} {
		data, err := OSFS{}.ReadFileMax(filepath.Join(dir, "f"), tc.max)
		if !errors.Is(err, tc.want) || (tc.want == nil && string(data) != "12345") {
			t.Errorf("ReadFileMax(%d) = %q, %v; want %v", tc.max, data, err, tc.want)
		}
	}
	if _, err := (OSFS{}).ReadFileMax(filepath.Join(dir, "missing"), 5); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
	// A directory opens and cannot be read: the error is the file system's.
	if _, err := (OSFS{}).ReadFileMax(dir, 5); err == nil {
		t.Error("a directory is not a file")
	}
}
