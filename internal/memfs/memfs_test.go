package memfs

import (
	"errors"
	"io/fs"
	"testing"
)

func TestFS(t *testing.T) {
	t.Parallel()
	m := New(map[string]string{"/work/a.yaml": "a", "work/sub/b.yaml": "b"})
	data, err := m.ReadFile("/work/./a.yaml")
	if err != nil || string(data) != "a" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if _, err := m.ReadFile("work/missing"); err == nil {
		t.Fatal("a missing file must fail")
	}
	if info, err := m.Stat("/work/sub"); err != nil || !info.IsDir() {
		t.Fatalf("Stat dir = %v, %v", info, err)
	}
	if _, err := m.Stat("/nope"); err == nil {
		t.Fatal("Stat of a missing path must fail")
	}
	linked := New(map[string]string{"/work/a.yaml": "a", "/work/link.yaml": "a"}, "/work/link.yaml")
	if list, err := linked.ReadDir("/work"); err != nil || !list[0].Type().IsRegular() || list[1].Type()&fs.ModeSymlink == 0 {
		t.Fatalf("symlinks = %v, %v", list, err)
	}
	entries, err := m.ReadDir("/work")
	if err != nil || len(entries) != 2 || entries[0].Name() != "a.yaml" || !entries[1].IsDir() {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}
}

func TestReadFileMaxReadsAtMostMaxBytes(t *testing.T) {
	t.Parallel()
	fsys := New(map[string]string{"/d/f": "12345"})
	for _, tc := range []struct {
		max  int64
		want error
	}{{5, nil}, {9, nil}, {4, ErrTooLarge}} {
		data, err := fsys.ReadFileMax("/d/f", tc.max)
		if !errors.Is(err, tc.want) || (tc.want == nil && string(data) != "12345") {
			t.Errorf("ReadFileMax(%d) = %q, %v; want %v", tc.max, data, err, tc.want)
		}
	}
	if _, err := fsys.ReadFileMax("/d/missing", 5); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
}
