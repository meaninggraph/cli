package memfs

import (
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
