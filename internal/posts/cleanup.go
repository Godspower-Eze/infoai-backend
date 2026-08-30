package posts

import (
	"context"
	"errors"
)

type CleanupRepository interface {
	CompleteStorageDeletion(context.Context, string) error
	FailStorageDeletion(context.Context, string, string) error
}

type Cleanup struct {
	repository CleanupRepository
	storage    MediaStorage
}

func NewCleanup(repository CleanupRepository, storage MediaStorage) *Cleanup {
	return &Cleanup{repository: repository, storage: storage}
}
func (c *Cleanup) Run(ctx context.Context, args StorageCleanupArgs) error {
	if args.StorageKey == "" {
		return errors.New("storage cleanup key is required")
	}
	if err := c.storage.Delete(ctx, args.StorageKey); err != nil {
		return errors.Join(err, c.repository.FailStorageDeletion(ctx, args.StorageKey, "storage_delete_failed"))
	}
	return c.repository.CompleteStorageDeletion(ctx, args.StorageKey)
}
