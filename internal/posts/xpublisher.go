package posts

import (
	"context"
	"io"
)

type MediaUpload struct {
	Filename string
	MIMEType string
	Category MediaCategory
	Size     int64
	AltText  *string
	Source   io.Reader
}

type XPostInput struct {
	Text      string
	MediaIDs  []string
	ReplyToID string
}

type XPublisher interface {
	UploadMedia(context.Context, string, MediaUpload) (string, error)
	CreatePost(context.Context, string, XPostInput) (string, error)
	DeletePost(context.Context, string, string) error
}
