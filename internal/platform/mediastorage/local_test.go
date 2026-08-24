package mediastorage

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoragePutIsAtomicAndReturnsChecksum(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}

	object, err := store.Put(context.Background(), "ab/asset-id", strings.NewReader("media"))
	if err != nil {
		t.Fatal(err)
	}
	wantSum := sha256.Sum256([]byte("media"))
	if object.Key != "ab/asset-id" || object.Size != 5 || !equalBytes(object.SHA256, wantSum[:]) {
		t.Fatalf("Put() object = %+v", object)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "ab")); err != nil || len(entries) != 1 || entries[0].Name() != "asset-id" {
		t.Fatalf("destination entries = %v, error = %v", entries, err)
	}
}

func TestLocalStorageRejectsUnsafeKeys(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"", "/absolute", "../outside", "safe/../outside", "safe//asset", "./asset", `safe\asset`} {
		t.Run(key, func(t *testing.T) {
			if _, err := store.Put(context.Background(), key, strings.NewReader("media")); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Put(%q) error = %v, want ErrInvalidKey", key, err)
			}
		})
	}
}

func TestLocalStorageInterruptedPutLeavesNoObjectOrTemporaryFile(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Put(context.Background(), "ab/asset-id", &failingReader{data: []byte("partial")})
	if err == nil {
		t.Fatal("Put() error = nil, want interrupted-reader error")
	}
	entries, readErr := os.ReadDir(filepath.Join(root, "ab"))
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("interrupted Put() left entries: %v", entries)
	}
}

func TestLocalStorageOpenReturnsStoredContent(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "ab/asset-id", strings.NewReader("media")); err != nil {
		t.Fatal(err)
	}

	reader, err := store.Open(context.Background(), "ab/asset-id")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "media" {
		t.Fatalf("Open() content = %q, want media", got)
	}
}

func TestLocalStorageDeleteIsIdempotent(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "ab/asset-id", strings.NewReader("media")); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(context.Background(), "ab/asset-id"); err != nil {
		t.Fatalf("first Delete() error = %v", err)
	}
	if err := store.Delete(context.Background(), "ab/asset-id"); err != nil {
		t.Fatalf("missing Delete() error = %v", err)
	}
}

func TestLocalStorageUsesRestrictivePermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "media-root")
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "ab/asset-id", strings.NewReader("media")); err != nil {
		t.Fatal(err)
	}

	for _, directory := range []string{root, filepath.Join(root, "ab")} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("directory %q mode = %o, want 700", directory, got)
		}
	}
	info, err := os.Stat(filepath.Join(root, "ab", "asset-id"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("object mode = %o, want 600", got)
	}
}

func TestNewLocalRejectsEmptyRoot(t *testing.T) {
	if _, err := NewLocal(" "); err == nil {
		t.Fatal("NewLocal() error = nil, want invalid root error")
	}
}

type failingReader struct {
	data []byte
	done bool
}

func (r *failingReader) Read(destination []byte) (int, error) {
	if r.done {
		return 0, errors.New("reader interrupted")
	}
	r.done = true
	return copy(destination, r.data), nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
