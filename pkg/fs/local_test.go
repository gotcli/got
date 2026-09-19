package fs

import (
	"path/filepath"
	"testing"
)

func TestLocalFSWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	local := LocalFS{}
	if err := local.WriteFile(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	content, err := local.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello" {
		t.Fatalf("got %q", content)
	}
	if !FileExist(path) {
		t.Fatal("expected file to exist")
	}
}
