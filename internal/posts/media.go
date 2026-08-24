package posts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
)

const (
	maxImageBytes = 5 << 20
	maxGIFBytes   = 15 << 20
	maxVideoBytes = 512 << 20
)

var (
	ErrUnsupportedMedia        = errors.New("unsupported media")
	ErrInvalidMediaSize        = errors.New("invalid media size")
	ErrMediaTooLarge           = errors.New("media is too large")
	ErrTooManyMedia            = errors.New("too many media files")
	ErrInvalidMediaCombination = errors.New("invalid media combination")
)

type MediaCategory string

const (
	MediaImage MediaCategory = "image"
	MediaGIF   MediaCategory = "gif"
	MediaVideo MediaCategory = "video"
)

type MediaMetadata struct {
	Category MediaCategory
}

type DetectedMedia struct {
	Category MediaCategory
	MIMEType string
	Size     int64
}

type StoredObject struct {
	Key    string
	Size   int64
	SHA256 []byte
}

type MediaStorage interface {
	Put(context.Context, string, io.Reader) (StoredObject, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type MediaPolicy interface {
	Validate(header []byte, size int64, siblings []MediaMetadata) (DetectedMedia, error)
}

type defaultMediaPolicy struct{}

func DefaultMediaPolicy() MediaPolicy {
	return defaultMediaPolicy{}
}

func (defaultMediaPolicy) Validate(header []byte, size int64, siblings []MediaMetadata) (DetectedMedia, error) {
	if size <= 0 {
		return DetectedMedia{}, ErrInvalidMediaSize
	}

	detected, limit, ok := detectMedia(header)
	if !ok {
		return DetectedMedia{}, ErrUnsupportedMedia
	}
	if size > limit {
		return DetectedMedia{}, ErrMediaTooLarge
	}
	if err := validateMediaCombination(detected.Category, siblings); err != nil {
		return DetectedMedia{}, err
	}
	detected.Size = size
	return detected, nil
}

func detectMedia(header []byte) (DetectedMedia, int64, bool) {
	detectedMIME := http.DetectContentType(header)
	switch {
	case detectedMIME == "image/jpeg" && len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff}):
		return DetectedMedia{Category: MediaImage, MIMEType: "image/jpeg"}, maxImageBytes, true
	case detectedMIME == "image/png" && len(header) >= 8 && bytes.Equal(header[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return DetectedMedia{Category: MediaImage, MIMEType: "image/png"}, maxImageBytes, true
	case detectedMIME == "image/webp" && len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP")):
		return DetectedMedia{Category: MediaImage, MIMEType: "image/webp"}, maxImageBytes, true
	case detectedMIME == "image/gif" && len(header) >= 6 && (bytes.Equal(header[:6], []byte("GIF87a")) || bytes.Equal(header[:6], []byte("GIF89a"))):
		return DetectedMedia{Category: MediaGIF, MIMEType: "image/gif"}, maxGIFBytes, true
	case detectedMIME == "video/mp4" && isMP4(header):
		return DetectedMedia{Category: MediaVideo, MIMEType: "video/mp4"}, maxVideoBytes, true
	}
	return DetectedMedia{}, 0, false
}

func isMP4(header []byte) bool {
	if len(header) < 12 || !bytes.Equal(header[4:8], []byte("ftyp")) {
		return false
	}
	boxSize := int64(header[0])<<24 | int64(header[1])<<16 | int64(header[2])<<8 | int64(header[3])
	return boxSize >= 12
}

func validateMediaCombination(incoming MediaCategory, siblings []MediaMetadata) error {
	if incoming == MediaImage {
		if len(siblings) >= 4 {
			return ErrTooManyMedia
		}
		for _, sibling := range siblings {
			if sibling.Category != MediaImage {
				return ErrInvalidMediaCombination
			}
		}
		return nil
	}

	if len(siblings) > 0 {
		return ErrInvalidMediaCombination
	}
	return nil
}
