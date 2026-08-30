package xintegration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"testing"

	"github.com/Godspower-Eze/infoai-backend/internal/posts"
)

type publishRoundTripFunc func(*http.Request) (*http.Response, error)

func (f publishRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newPublisherTestClient(transport http.RoundTripper) *XClient {
	return NewXClient("client", "secret", "https://example.test/callback", &http.Client{Transport: transport}, XEndpoints{APIBaseURL: "https://api.test"})
}

func TestCreatePostClassifiesLostResponseAfterWriteAsAmbiguous(t *testing.T) {
	client := newPublisherTestClient(publishRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.WroteRequest != nil {
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		}
		return nil, io.ErrUnexpectedEOF
	}))
	_, err := client.CreatePost(context.Background(), "secret-token", posts.XPostInput{Text: "hello"})
	if ClassificationOf(err) != Ambiguous {
		t.Fatalf("classification = %v, error = %v", ClassificationOf(err), err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatal("error leaked access token")
	}
}

func TestCreatePostParsesIDAndBuildsReply(t *testing.T) {
	client := newPublisherTestClient(publishRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if request.Method != http.MethodPost || request.URL.Path != "/2/tweets" || !strings.Contains(string(body), `"in_reply_to_tweet_id":"100"`) {
			t.Fatalf("request = %s %s %s", request.Method, request.URL.Path, body)
		}
		return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"id":"101"}}`))}, nil
	}))
	id, err := client.CreatePost(context.Background(), "token", posts.XPostInput{Text: "reply", ReplyToID: "100"})
	if err != nil || id != "101" {
		t.Fatalf("id/error = %q/%v", id, err)
	}
}

func TestDeletePostTreatsNotFoundAsAlreadyDeleted(t *testing.T) {
	client := newPublisherTestClient(publishRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found"))}, nil
	}))
	if err := client.DeletePost(context.Background(), "token", "101"); err != nil {
		t.Fatal(err)
	}
}

func TestPublisherStatusClassification(t *testing.T) {
	for status, want := range map[int]FailureClassification{401: Reauthorization, 403: Reauthorization, 429: DefiniteRetryable, 500: DefiniteRetryable, 400: Permanent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := newPublisherTestClient(publishRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("provider body must not escape"))}, nil
			}))
			_, err := client.CreatePost(context.Background(), "token", posts.XPostInput{Text: "test"})
			if ClassificationOf(err) != want {
				t.Fatalf("classification = %v, want %v", ClassificationOf(err), want)
			}
			if errors.Is(err, context.Canceled) {
				t.Fatal("unexpected cancellation")
			}
			if strings.Contains(err.Error(), "provider body") {
				t.Fatal("error leaked provider body")
			}
		})
	}
}
