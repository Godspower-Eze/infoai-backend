package httpapi

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/posts"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxMediaRequestBytes int64 = (512 << 20) + (1 << 20)

type postItemRequest struct {
	ID   *uuid.UUID `json:"id,omitempty"`
	Text string     `json:"text"`
}
type createPostRequest struct {
	XAccountID uuid.UUID         `json:"x_account_id"`
	Items      []postItemRequest `json:"items"`
}
type updatePostRequest struct {
	Items []postItemRequest `json:"items"`
}
type schedulePostRequest struct {
	ScheduledAt time.Time `json:"scheduled_at"`
}
type resolveOutcomeRequest struct {
	Decision posts.OutcomeDecision `json:"decision"`
	XURL     string                `json:"x_url,omitempty"`
}
type deletePostRequest struct {
	ConfirmXDeletion bool `json:"confirm_x_deletion"`
}

func (api *API) resolvePostOutcome(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	itemID, ok := pathUUID(w, r, "itemID")
	if !ok {
		return
	}
	var body resolveOutcomeRequest
	if decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.", nil)
		return
	}
	updated, err := api.posts.ResolveOutcome(r.Context(), posts.ResolveOutcomeCommand{OwnerID: requestUserID(r), PostID: postID, ItemID: itemID, Decision: body.Decision, XURL: body.XURL})
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, postResponse(updated))
}

func (api *API) publishPost(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	queued, err := api.posts.Publish(r.Context(), requestUserID(r), postID)
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, postResponse(queued))
}

func (api *API) retryPost(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	queued, err := api.posts.Retry(r.Context(), posts.RetryCommand{OwnerID: requestUserID(r), PostID: postID})
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, postResponse(queued))
}

func (api *API) schedulePost(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	var body schedulePostRequest
	if decodeJSON(r, &body) != nil || body.ScheduledAt.IsZero() {
		writeError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.", nil)
		return
	}
	queued, err := api.posts.Schedule(r.Context(), requestUserID(r), postID, body.ScheduledAt)
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, postResponse(queued))
}

func (api *API) cancelPostSchedule(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	updated, err := api.posts.CancelSchedule(r.Context(), requestUserID(r), postID)
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, postResponse(updated))
}

func (api *API) createPost(w http.ResponseWriter, r *http.Request) {
	var body createPostRequest
	if decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.", nil)
		return
	}
	created, err := api.posts.Create(r.Context(), posts.CreateCommand{OwnerID: requestUserID(r), XAccountID: body.XAccountID, CreationMode: posts.CreationModeUser, Items: itemInputs(body.Items)})
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, postResponse(created))
}

func (api *API) listPosts(w http.ResponseWriter, r *http.Request) {
	listed, err := api.posts.List(r.Context(), requestUserID(r))
	if err != nil {
		writePostError(w, err)
		return
	}
	result := make([]any, len(listed))
	for i := range listed {
		result[i] = postResponse(listed[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": result})
}

func (api *API) getPost(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	post, err := api.posts.Get(r.Context(), requestUserID(r), id)
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, postResponse(post))
}

func (api *API) updatePost(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	var body updatePostRequest
	if decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.", nil)
		return
	}
	updated, err := api.posts.Update(r.Context(), posts.UpdateCommand{OwnerID: requestUserID(r), PostID: id, Items: itemInputs(body.Items)})
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, postResponse(updated))
}

func (api *API) deletePost(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	var body deletePostRequest
	if r.Body != nil && r.ContentLength != 0 && decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.", nil)
		return
	}
	queued, err := api.posts.RequestDeletion(r.Context(), posts.DeleteCommand{OwnerID: requestUserID(r), PostID: id, ConfirmXDeletion: body.ConfirmXDeletion})
	if err != nil {
		writePostError(w, err)
		return
	}
	if queued {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) uploadPostMedia(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	itemID, ok := pathUUID(w, r, "itemID")
	if !ok {
		return
	}
	if r.ContentLength > maxMediaRequestBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "media_too_large", "The media upload is too large.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaRequestBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeMultipartError(w, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required", "A media file is required.", nil)
		return
	}
	defer file.Close()
	prefix := make([]byte, 512)
	count, err := io.ReadFull(file, prefix)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		writeError(w, http.StatusBadRequest, "invalid_media", "The media file could not be read.", nil)
		return
	}
	prefix = prefix[:count]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_media", "The media file could not be read.", nil)
		return
	}
	var alt *string
	if value := r.FormValue("alt_text"); value != "" {
		alt = &value
	}
	updated, err := api.posts.UploadMedia(r.Context(), posts.UploadMediaCommand{OwnerID: requestUserID(r), PostID: postID, ItemID: itemID, OriginalFilename: header.Filename, AltText: alt, Header: prefix, Size: header.Size, Source: file})
	if err != nil {
		writePostError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, postResponse(updated))
}

func (api *API) removePostMedia(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	itemID, ok := pathUUID(w, r, "itemID")
	if !ok {
		return
	}
	mediaID, ok := pathUUID(w, r, "mediaID")
	if !ok {
		return
	}
	if err := api.posts.RemoveMedia(r.Context(), posts.RemoveMediaCommand{OwnerID: requestUserID(r), PostID: postID, ItemID: itemID, MediaID: mediaID}); err != nil {
		writePostError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func itemInputs(items []postItemRequest) []posts.ItemInput {
	result := make([]posts.ItemInput, len(items))
	for i, item := range items {
		if item.ID != nil {
			result[i].ID = *item.ID
		}
		result[i].Text = item.Text
	}
	return result
}
func pathUUID(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "The resource identifier is invalid.", nil)
		return uuid.Nil, false
	}
	return id, true
}

func writePostError(w http.ResponseWriter, err error) {
	var field *posts.FieldError
	switch {
	case errors.As(err, &field):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "The post is invalid.", map[string]string{field.Field: field.Code})
	case errors.Is(err, posts.ErrNotFound), errors.Is(err, posts.ErrAccountNotFound):
		writeError(w, http.StatusNotFound, "post_not_found", "The post was not found.", nil)
	case errors.Is(err, posts.ErrNotEditable):
		writeError(w, http.StatusConflict, "post_not_editable", "The post is not editable.", nil)
	case errors.Is(err, posts.ErrConfirmationRequired):
		writeError(w, http.StatusConflict, "x_deletion_confirmation_required", "Confirm deletion of the known posts from X.", nil)
	case errors.Is(err, posts.ErrInvalidTransition), errors.Is(err, posts.ErrOutcomeUnknown):
		writeError(w, http.StatusConflict, "invalid_post_transition", "The post cannot make that transition.", nil)
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.", nil)
	}
}

func writeMultipartError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "media_too_large", "The media upload is too large.", nil)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_multipart", "The media upload is invalid.", nil)
}

func postResponse(post posts.Post) any {
	items := make([]any, len(post.Items))
	for i, item := range post.Items {
		media := make([]any, len(item.Media))
		for j, asset := range item.Media {
			media[j] = map[string]any{"id": asset.ID, "position": asset.Position, "original_filename": asset.OriginalFilename, "mime_type": asset.MIMEType, "size": asset.Size, "alt_text": asset.AltText}
		}
		items[i] = map[string]any{"id": item.ID, "position": item.Position, "text": item.Text, "x_post_id": item.XPostID, "submission_state": item.SubmissionState, "media": media}
	}
	return map[string]any{"id": post.ID, "x_account_id": post.XAccountID, "creation_mode": post.CreationMode, "status": post.Status, "scheduled_at": post.ScheduledAt, "active_job_id": post.ActiveJobID, "state_version": post.StateVersion, "items": items, "created_at": post.CreatedAt, "updated_at": post.UpdatedAt}
}
