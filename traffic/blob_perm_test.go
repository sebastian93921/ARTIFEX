package traffic

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSpillBlobIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	tr := &Traffic{dir: dir}
	body := bytes.Repeat([]byte("a"), maxInlineBody+1)
	got := tr.spill(body, "text/plain")
	if got.hash == "" {
		t.Fatal("expected a spilled blob")
	}
	blobDir := filepath.Join(dir, "_blobs", "sha256", got.hash[:2])
	blobPath := filepath.Join(blobDir, got.hash+".bin")
	info, err := os.Stat(blobPath)
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(blobDir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("blob mode %o, want 0600", info.Mode().Perm())
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("blob dir mode %o, want 0700", dirInfo.Mode().Perm())
	}
}
