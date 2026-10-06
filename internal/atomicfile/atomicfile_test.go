package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infkf/vitrina/internal/atomicfile"
)

func TestWriteReplacesFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := atomicfile.Write(path, []byte("first"), 0600); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}
	if err := atomicfile.Write(path, []byte("second"), 0600); err != nil {
		t.Fatalf("replacement write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("got %q, want second", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("got mode %o, want 0600", info.Mode().Perm())
	}
}
