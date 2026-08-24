package mediastorage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godspower-Eze/infoai-backend/internal/posts"
)

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrInvalidRoot
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve media storage root: %w", err)
	}
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create media storage root: %w", err)
	}
	if err := os.Chmod(absoluteRoot, 0o700); err != nil {
		return nil, fmt.Errorf("secure media storage root: %w", err)
	}
	return &Local{root: absoluteRoot}, nil
}

func (s *Local) Put(ctx context.Context, key string, source io.Reader) (object posts.StoredObject, returnedErr error) {
	path, err := s.pathFor(key)
	if err != nil {
		return posts.StoredObject{}, err
	}
	if source == nil {
		return posts.StoredObject{}, fmt.Errorf("store media object: source is nil")
	}
	if err := ctx.Err(); err != nil {
		return posts.StoredObject{}, err
	}

	directory := filepath.Dir(path)
	if err := s.createDirectories(directory); err != nil {
		return posts.StoredObject{}, err
	}
	temporary, err := os.CreateTemp(directory, ".media-*")
	if err != nil {
		return posts.StoredObject{}, fmt.Errorf("create temporary media object: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if temporary != nil {
			if err := temporary.Close(); returnedErr == nil && err != nil {
				returnedErr = fmt.Errorf("close temporary media object: %w", err)
			}
		}
		if returnedErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return posts.StoredObject{}, fmt.Errorf("secure temporary media object: %w", err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temporary, hash), contextReader{ctx: ctx, source: source})
	if err != nil {
		return posts.StoredObject{}, fmt.Errorf("write media object: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return posts.StoredObject{}, fmt.Errorf("sync media object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return posts.StoredObject{}, fmt.Errorf("close media object: %w", err)
	}
	temporary = nil
	if err := os.Rename(temporaryPath, path); err != nil {
		return posts.StoredObject{}, fmt.Errorf("publish media object: %w", err)
	}

	return posts.StoredObject{Key: key, Size: size, SHA256: hash.Sum(nil)}, nil
}

func (s *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	path, err := s.pathFor(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open media object: %w", err)
	}
	return file, nil
}

func (s *Local) Delete(ctx context.Context, key string) error {
	path, err := s.pathFor(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete media object: %w", err)
	}
	return nil
}

func (s *Local) pathFor(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return "", ErrInvalidKey
	}
	components := strings.Split(key, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, 0) {
			return "", ErrInvalidKey
		}
	}
	return filepath.Join(append([]string{s.root}, components...)...), nil
}

func (s *Local) createDirectories(destination string) error {
	relative, err := filepath.Rel(s.root, destination)
	if err != nil {
		return fmt.Errorf("resolve media object directory: %w", err)
	}
	current := s.root
	if relative == "." {
		return nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create media object directory: %w", err)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect media object directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("media object directory is unsafe")
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return fmt.Errorf("secure media object directory: %w", err)
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(destination []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(destination)
}

var _ posts.MediaStorage = (*Local)(nil)
