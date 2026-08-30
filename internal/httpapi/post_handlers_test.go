package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/posts"
	"github.com/google/uuid"
)

func TestCreatePostRequiresAuthentication(t *testing.T) {
	api := newTestAPI(t)
	response := request(t, api.handler, http.MethodPost, "/api/v1/posts", map[string]any{"x_account_id": uuid.New(), "items": []any{map[string]string{"text": "draft"}}}, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCreatePostAllowsEmptyItemAndRejectsUnknownFields(t *testing.T) {
	api := newTestAPI(t)
	cookie := signup(t, api)
	accountID := uuid.New()
	api.posts.created = posts.Post{ID: uuid.New(), OwnerID: api.auth.user.ID, XAccountID: accountID, Status: posts.StatusDraft, Items: []posts.Item{{ID: uuid.New(), Position: 0}}}

	created := request(t, api.handler, http.MethodPost, "/api/v1/posts", map[string]any{"x_account_id": accountID, "items": []any{map[string]string{"text": ""}}}, cookie)
	if created.Code != http.StatusCreated || !bytes.Contains(created.Body.Bytes(), []byte(`"text":""`)) {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	if api.posts.create.OwnerID != api.auth.user.ID || api.posts.create.Items[0].Text != "" {
		t.Fatalf("Create command = %+v", api.posts.create)
	}
	unknown := request(t, api.handler, http.MethodPost, "/api/v1/posts", map[string]any{"x_account_id": accountID, "items": []any{map[string]string{"text": "draft"}}, "status": "published"}, cookie)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown-field status = %d", unknown.Code)
	}
}

func TestPostCRUDDelegatesOwnerAndReturnsOrderedItems(t *testing.T) {
	api := newTestAPI(t)
	cookie := signup(t, api)
	postID := uuid.New()
	xPostID := "1891234567890"
	scheduledAt := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	api.posts.post = posts.Post{ID: postID, OwnerID: api.auth.user.ID, Status: posts.StatusScheduled, ScheduledAt: &scheduledAt, Items: []posts.Item{{ID: uuid.New(), Position: 0, Text: "first", XPostID: &xPostID}, {ID: uuid.New(), Position: 1, Text: "second"}}}
	api.posts.listed = []posts.Post{api.posts.post}

	listed := request(t, api.handler, http.MethodGet, "/api/v1/posts", nil, cookie)
	got := request(t, api.handler, http.MethodGet, "/api/v1/posts/"+postID.String(), nil, cookie)
	updated := request(t, api.handler, http.MethodPatch, "/api/v1/posts/"+postID.String(), map[string]any{"items": []any{map[string]any{"id": api.posts.post.Items[0].ID, "text": "changed"}}}, cookie)
	deleted := request(t, api.handler, http.MethodDelete, "/api/v1/posts/"+postID.String(), nil, cookie)
	if listed.Code != http.StatusOK || got.Code != http.StatusOK || updated.Code != http.StatusOK || deleted.Code != http.StatusNoContent {
		t.Fatalf("statuses list/get/update/delete = %d/%d/%d/%d", listed.Code, got.Code, updated.Code, deleted.Code)
	}
	if !bytes.Contains(got.Body.Bytes(), []byte(`"text":"first"`)) || api.posts.ownerID != api.auth.user.ID || api.posts.postID != postID {
		t.Fatalf("get body = %s, owner/post = %s/%s", got.Body.String(), api.posts.ownerID, api.posts.postID)
	}
	if !bytes.Contains(got.Body.Bytes(), []byte(`"scheduled_at":"2026-08-25T12:00:00Z"`)) || !bytes.Contains(got.Body.Bytes(), []byte(`"x_post_id":"1891234567890"`)) {
		t.Fatalf("lifecycle fields missing from response: %s", got.Body.String())
	}
}

func TestPostErrorsUseStableMappings(t *testing.T) {
	api := newTestAPI(t)
	cookie := signup(t, api)
	api.posts.err = &posts.FieldError{Field: "items[0].text", Code: "too_long", Err: posts.ErrTextTooLong}
	response := request(t, api.handler, http.MethodPost, "/api/v1/posts", map[string]any{"x_account_id": uuid.New(), "items": []any{map[string]string{"text": "long"}}}, cookie)
	if response.Code != http.StatusUnprocessableEntity || !bytes.Contains(response.Body.Bytes(), []byte(`"items[0].text":"too_long"`)) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPostMediaRoutesAreItemScopedAndMultipartBounded(t *testing.T) {
	api := newTestAPI(t)
	cookie := signup(t, api)
	postID, itemID, mediaID := uuid.New(), uuid.New(), uuid.New()
	api.posts.post = posts.Post{ID: postID, Items: []posts.Item{{ID: itemID, Media: []posts.Media{{ID: mediaID, MIMEType: "image/png"}}}}}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	_ = writer.WriteField("alt_text", "description")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+postID.String()+"/items/"+itemID.String()+"/media", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", testFrontendOrigin)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, req)
	removed := request(t, api.handler, http.MethodDelete, "/api/v1/posts/"+postID.String()+"/items/"+itemID.String()+"/media/"+mediaID.String(), nil, cookie)
	if response.Code != http.StatusCreated || removed.Code != http.StatusNoContent {
		t.Fatalf("upload/remove statuses = %d/%d; upload body = %s", response.Code, removed.Code, response.Body.String())
	}
	if api.posts.upload.PostID != postID || api.posts.upload.ItemID != itemID || api.posts.remove.MediaID != mediaID {
		t.Fatalf("media commands = %+v / %+v", api.posts.upload, api.posts.remove)
	}

	tooLarge := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+postID.String()+"/items/"+itemID.String()+"/media", http.NoBody)
	tooLarge.ContentLength = maxMediaRequestBytes + 1
	tooLarge.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	tooLarge.Header.Set("Origin", testFrontendOrigin)
	tooLarge.AddCookie(cookie)
	limited := httptest.NewRecorder()
	api.handler.ServeHTTP(limited, tooLarge)
	if limited.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d", limited.Code)
	}
}

func TestWriteMultipartErrorMapsBodyLimitSeparatelyFromMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "body limit", err: &http.MaxBytesError{Limit: 64}, want: http.StatusRequestEntityTooLarge, code: `"code":"media_too_large"`},
		{name: "malformed multipart", err: errors.New("missing boundary"), want: http.StatusBadRequest, code: `"code":"invalid_multipart"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeMultipartError(response, test.err)
			if response.Code != test.want || !bytes.Contains(response.Body.Bytes(), []byte(test.code)) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

type stubPosts struct {
	created posts.Post
	post    posts.Post
	listed  []posts.Post
	err     error
	create  posts.CreateCommand
	ownerID uuid.UUID
	postID  uuid.UUID
	upload  posts.UploadMediaCommand
	remove  posts.RemoveMediaCommand
}

func (s *stubPosts) Create(_ context.Context, command posts.CreateCommand) (posts.Post, error) {
	s.create = command
	return s.created, s.err
}
func (s *stubPosts) Get(_ context.Context, ownerID, postID uuid.UUID) (posts.Post, error) {
	s.ownerID, s.postID = ownerID, postID
	return s.post, s.err
}
func (s *stubPosts) List(context.Context, uuid.UUID) ([]posts.Post, error) { return s.listed, s.err }
func (s *stubPosts) Update(_ context.Context, command posts.UpdateCommand) (posts.Post, error) {
	s.ownerID, s.postID = command.OwnerID, command.PostID
	return s.post, s.err
}
func (s *stubPosts) Delete(_ context.Context, ownerID, postID uuid.UUID) error {
	s.ownerID, s.postID = ownerID, postID
	return s.err
}
func (s *stubPosts) RequestDeletion(_ context.Context, command posts.DeleteCommand) (bool, error) {
	s.ownerID, s.postID = command.OwnerID, command.PostID
	return false, s.err
}
func (s *stubPosts) UploadMedia(_ context.Context, command posts.UploadMediaCommand) (posts.Post, error) {
	s.upload = command
	return s.post, s.err
}
func (s *stubPosts) RemoveMedia(_ context.Context, command posts.RemoveMediaCommand) error {
	s.remove = command
	return s.err
}
func (s *stubPosts) Publish(_ context.Context, ownerID, postID uuid.UUID) (posts.Post, error) {
	s.ownerID, s.postID = ownerID, postID
	return s.post, s.err
}
func (s *stubPosts) Schedule(_ context.Context, ownerID, postID uuid.UUID, _ time.Time) (posts.Post, error) {
	s.ownerID, s.postID = ownerID, postID
	return s.post, s.err
}
func (s *stubPosts) CancelSchedule(_ context.Context, ownerID, postID uuid.UUID) (posts.Post, error) {
	s.ownerID, s.postID = ownerID, postID
	return s.post, s.err
}
func (s *stubPosts) ResolveOutcome(_ context.Context, command posts.ResolveOutcomeCommand) (posts.Post, error) {
	s.ownerID, s.postID = command.OwnerID, command.PostID
	return s.post, s.err
}
func (s *stubPosts) Retry(_ context.Context, command posts.RetryCommand) (posts.Post, error) {
	s.ownerID, s.postID = command.OwnerID, command.PostID
	return s.post, s.err
}

var _ io.Reader
