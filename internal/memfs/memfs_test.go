package memfs

import "testing"

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
	entries, err := m.ReadDir("/work")
	if err != nil || len(entries) != 2 || entries[0].Name() != "a.yaml" || !entries[1].IsDir() {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}
}
