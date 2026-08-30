package xintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/posts"
)

const maxXResponseBytes = 1 << 20

func (c *XClient) CreatePost(ctx context.Context, token string, input posts.XPostInput) (string, error) {
	payload := map[string]any{"text": input.Text}
	if len(input.MediaIDs) > 0 {
		payload["media"] = map[string]any{"media_ids": input.MediaIDs}
	}
	if input.ReplyToID != "" {
		payload["reply"] = map[string]any{"in_reply_to_tweet_id": input.ReplyToID}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	response, err := c.publishRequest(ctx, token, http.MethodPost, strings.TrimRight(c.endpoints.APIBaseURL, "/")+"/2/tweets", "application/json", bytes.NewReader(body), "create post")
	if err != nil {
		return "", err
	}
	var decoded struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(response, &decoded) != nil || decoded.Data.ID == "" {
		return "", &PublishError{Classification: Ambiguous, Operation: "create post", Cause: fmt.Errorf("malformed success response")}
	}
	return decoded.Data.ID, nil
}

func (c *XClient) DeletePost(ctx context.Context, token, postID string) error {
	_, err := c.publishRequest(ctx, token, http.MethodDelete, strings.TrimRight(c.endpoints.APIBaseURL, "/")+"/2/tweets/"+url.PathEscape(postID), "", nil, "delete post")
	var failure *PublishError
	if errorsAs(err, &failure) && failure.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *XClient) UploadMedia(ctx context.Context, token string, upload posts.MediaUpload) (string, error) {
	endpoint := c.endpoints.MediaUploadURL
	if endpoint == "" {
		endpoint = strings.TrimRight(c.endpoints.APIBaseURL, "/") + "/1.1/media/upload.json"
	}
	initValues := url.Values{"command": {"INIT"}, "total_bytes": {strconv.FormatInt(upload.Size, 10)}, "media_type": {upload.MIMEType}, "media_category": {mediaCategory(upload.Category)}}
	data, err := c.publishRequest(ctx, token, http.MethodPost, endpoint, "application/x-www-form-urlencoded", strings.NewReader(initValues.Encode()), "initialize media")
	if err != nil {
		return "", err
	}
	var initialized struct {
		MediaID string `json:"media_id_string"`
	}
	if json.Unmarshal(data, &initialized) != nil || initialized.MediaID == "" {
		return "", &PublishError{Classification: Permanent, Operation: "initialize media", Cause: fmt.Errorf("malformed response")}
	}
	buffer := make([]byte, 4<<20)
	segment := 0
	for {
		count, readErr := io.ReadFull(upload.Source, buffer)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return "", readErr
		}
		if count > 0 {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			_ = writer.WriteField("command", "APPEND")
			_ = writer.WriteField("media_id", initialized.MediaID)
			_ = writer.WriteField("segment_index", strconv.Itoa(segment))
			header := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="media"; filename="` + escapeFilename(upload.Filename) + `"`}, "Content-Type": {upload.MIMEType}}
			part, _ := writer.CreatePart(header)
			_, _ = part.Write(buffer[:count])
			_ = writer.Close()
			if _, err := c.publishRequest(ctx, token, http.MethodPost, endpoint, writer.FormDataContentType(), &body, "append media"); err != nil {
				return "", err
			}
			segment++
		}
		if readErr != nil {
			break
		}
	}
	finalValues := url.Values{"command": {"FINALIZE"}, "media_id": {initialized.MediaID}}
	finalized, err := c.publishRequest(ctx, token, http.MethodPost, endpoint, "application/x-www-form-urlencoded", strings.NewReader(finalValues.Encode()), "finalize media")
	if err != nil {
		return "", err
	}
	if err := c.waitForMedia(ctx, token, endpoint, initialized.MediaID, finalized); err != nil {
		return "", err
	}
	if upload.AltText != nil && *upload.AltText != "" {
		metadata, _ := json.Marshal(map[string]any{"media_id": initialized.MediaID, "alt_text": map[string]string{"text": *upload.AltText}})
		metadataURL := c.endpoints.MediaMetadataURL
		if metadataURL == "" {
			metadataURL = strings.TrimRight(c.endpoints.APIBaseURL, "/") + "/1.1/media/metadata/create.json"
		}
		if _, err := c.publishRequest(ctx, token, http.MethodPost, metadataURL, "application/json", bytes.NewReader(metadata), "set media metadata"); err != nil {
			return "", err
		}
	}
	return initialized.MediaID, nil
}

type mediaProcessingResponse struct {
	ProcessingInfo *struct {
		State          string `json:"state"`
		CheckAfterSecs int    `json:"check_after_secs"`
	} `json:"processing_info"`
}

func (c *XClient) waitForMedia(ctx context.Context, token, endpoint, mediaID string, data []byte) error {
	for {
		var state mediaProcessingResponse
		if len(data) > 0 && json.Unmarshal(data, &state) != nil {
			return &PublishError{Classification: Permanent, Operation: "process media", Cause: fmt.Errorf("malformed response")}
		}
		if state.ProcessingInfo == nil || state.ProcessingInfo.State == "succeeded" {
			return nil
		}
		if state.ProcessingInfo.State == "failed" {
			return &PublishError{Classification: Permanent, Operation: "process media", Cause: fmt.Errorf("media processing failed")}
		}
		delay := time.Duration(state.ProcessingInfo.CheckAfterSecs) * time.Second
		if delay <= 0 {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		statusURL := endpoint + "?" + url.Values{"command": {"STATUS"}, "media_id": {mediaID}}.Encode()
		var err error
		data, err = c.publishRequest(ctx, token, http.MethodGet, statusURL, "", nil, "check media")
		if err != nil {
			return err
		}
	}
}

func (c *XClient) publishRequest(ctx context.Context, token, method, endpoint, contentType string, body io.Reader, operation string) ([]byte, error) {
	wrote := false
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wrote = true }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		classification := DefiniteRetryable
		if wrote && (method == http.MethodPost || method == http.MethodDelete) {
			classification = Ambiguous
		}
		return nil, &PublishError{Classification: classification, Operation: operation, Cause: err}
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxXResponseBytes+1))
	if readErr != nil || len(data) > maxXResponseBytes {
		return nil, &PublishError{Classification: Ambiguous, StatusCode: response.StatusCode, Operation: operation, Cause: fmt.Errorf("invalid response body")}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return data, nil
	}
	classification := Permanent
	if response.StatusCode == 401 || response.StatusCode == 403 {
		classification = Reauthorization
	}
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		classification = DefiniteRetryable
	}
	failure := &PublishError{Classification: classification, StatusCode: response.StatusCode, Operation: operation}
	if raw := response.Header.Get("x-rate-limit-reset"); raw != "" {
		if unix, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
			failure.RetryAt = time.Unix(unix, 0)
		}
	}
	return nil, failure
}

func mediaCategory(category posts.MediaCategory) string {
	switch category {
	case posts.MediaGIF:
		return "tweet_gif"
	case posts.MediaVideo:
		return "tweet_video"
	default:
		return "tweet_image"
	}
}
func escapeFilename(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `"`, ""), "\r", "")
}
func errorsAs(err error, target any) bool { return errors.As(err, target) }

var _ posts.XPublisher = (*XClient)(nil)
