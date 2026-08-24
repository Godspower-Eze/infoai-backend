package posts

import (
	"errors"
	"testing"
)

var (
	jpegHeader = []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	pngHeader  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	webpHeader = []byte{'R', 'I', 'F', 'F', 0x0c, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}
	gifHeader  = []byte{'G', 'I', 'F', '8', '9', 'a'}
	mp4Header  = []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0x00, 0x00, 0x00, 0x00, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
)

func TestMediaPolicyDetectsSupportedSignatures(t *testing.T) {
	tests := []struct {
		name     string
		header   []byte
		wantType string
		wantKind MediaCategory
	}{
		{name: "JPEG", header: jpegHeader, wantType: "image/jpeg", wantKind: MediaImage},
		{name: "PNG", header: pngHeader, wantType: "image/png", wantKind: MediaImage},
		{name: "WebP", header: webpHeader, wantType: "image/webp", wantKind: MediaImage},
		{name: "GIF", header: gifHeader, wantType: "image/gif", wantKind: MediaGIF},
		{name: "MP4", header: mp4Header, wantType: "video/mp4", wantKind: MediaVideo},
	}

	policy := DefaultMediaPolicy()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := policy.Validate(test.header, 1024, nil)
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if got.MIMEType != test.wantType || got.Category != test.wantKind || got.Size != 1024 {
				t.Fatalf("Validate() = %+v, want MIME %q, category %q, size 1024", got, test.wantType, test.wantKind)
			}
		})
	}
}

func TestMediaPolicyEnforcesSizeBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		limit  int64
	}{
		{name: "image", header: pngHeader, limit: 5 << 20},
		{name: "GIF", header: gifHeader, limit: 15 << 20},
		{name: "video", header: mp4Header, limit: 512 << 20},
	}

	policy := DefaultMediaPolicy()
	for _, test := range tests {
		t.Run(test.name+" accepts limit", func(t *testing.T) {
			if _, err := policy.Validate(test.header, test.limit, nil); err != nil {
				t.Fatalf("Validate() at limit error = %v", err)
			}
		})
		t.Run(test.name+" rejects above limit", func(t *testing.T) {
			if _, err := policy.Validate(test.header, test.limit+1, nil); !errors.Is(err, ErrMediaTooLarge) {
				t.Fatalf("Validate() error = %v, want ErrMediaTooLarge", err)
			}
		})
	}
}

func TestMediaPolicyAllowsAtMostFourImages(t *testing.T) {
	policy := DefaultMediaPolicy()
	threeImages := []MediaMetadata{{Category: MediaImage}, {Category: MediaImage}, {Category: MediaImage}}
	if _, err := policy.Validate(pngHeader, 1024, threeImages); err != nil {
		t.Fatalf("Validate() fourth image error = %v", err)
	}

	fourImages := append(threeImages, MediaMetadata{Category: MediaImage})
	if _, err := policy.Validate(pngHeader, 1024, fourImages); !errors.Is(err, ErrTooManyMedia) {
		t.Fatalf("Validate() fifth image error = %v, want ErrTooManyMedia", err)
	}
}

func TestMediaPolicyRejectsVideoBesideImage(t *testing.T) {
	policy := DefaultMediaPolicy()
	_, err := policy.Validate(mp4Header, 1024, []MediaMetadata{{Category: MediaImage}})
	if !errors.Is(err, ErrInvalidMediaCombination) {
		t.Fatalf("Validate() error = %v, want ErrInvalidMediaCombination", err)
	}
}

func TestMediaPolicyRejectsImageBesideGIF(t *testing.T) {
	policy := DefaultMediaPolicy()
	_, err := policy.Validate(pngHeader, 1024, []MediaMetadata{{Category: MediaGIF}})
	if !errors.Is(err, ErrInvalidMediaCombination) {
		t.Fatalf("Validate() error = %v, want ErrInvalidMediaCombination", err)
	}
}

func TestMediaPolicyRejectsSecondExclusiveMedia(t *testing.T) {
	policy := DefaultMediaPolicy()
	for _, sibling := range []MediaCategory{MediaGIF, MediaVideo} {
		if _, err := policy.Validate(gifHeader, 1024, []MediaMetadata{{Category: sibling}}); !errors.Is(err, ErrInvalidMediaCombination) {
			t.Fatalf("Validate() beside %q error = %v, want ErrInvalidMediaCombination", sibling, err)
		}
	}
}

func TestMediaPolicyRejectsUnsupportedAndEmptyFiles(t *testing.T) {
	policy := DefaultMediaPolicy()
	if _, err := policy.Validate([]byte("plain text"), 10, nil); !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("Validate() unsupported error = %v, want ErrUnsupportedMedia", err)
	}
	if _, err := policy.Validate(pngHeader, 0, nil); !errors.Is(err, ErrInvalidMediaSize) {
		t.Fatalf("Validate() empty error = %v, want ErrInvalidMediaSize", err)
	}
}
