# Post Publishing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver durable immediate and scheduled publishing of single X posts and threads with local media storage, bounded retries, user-resolved ambiguous outcomes, and confirmed deletion from X.

**Architecture:** HTTP handlers and River workers call focused application services. Services depend on narrow repository, queue, storage, X publisher, clock, and error-reporter ports; Ent, River, the local filesystem, X HTTP, and JSONL reporting implement those ports at composition roots.

**Tech Stack:** Go 1.26, Chi, Ent, Atlas, PostgreSQL/pgx, River v0.41.1, OAuth 2.0 PKCE, `log/slog`, X API v2.

**Spec:** `docs/superpowers/specs/2026-08-21-post-publishing-design.md`

## Global Constraints

- Complete one task, report changes/files/validation/issues, and wait for explicit user approval before committing.
- Use test-driven development: observe each focused test fail before adding its implementation.
- Preserve the handler → service → port → adapter dependency direction.
- Support at most 25 ordered items per thread.
- Permit JPEG/PNG/WebP images up to 5 MB, animated GIF up to 15 MB, and MP4 video up to 512 MB; one item may contain up to four images or one GIF/video.
- Retry definite transient publication failures at most three total attempts per manual cycle, with jittered delays near 30 seconds then 2 minutes.
- Never retry an ambiguous X creation automatically and never call X post lookup, timeline, or search endpoints.
- Treat user-supplied X URLs as attestations; validate format and local uniqueness only.
- Never log post text, tokens, sessions, raw media, local storage paths, or unfiltered provider bodies.
- Use UTC for persistence and require offsets on scheduled API timestamps.
- Automated tests must use fakes or local HTTP servers and must never call paid X endpoints.

---

### Task 1: Refine the persistence model for submission and deletion recovery

**Files:**
- Modify: `ent/schema/post.go`
- Modify: `ent/schema/post_item.go`
- Modify: `ent/schema/schema_test.go`
- Regenerate: `ent/*.go`, `ent/post/*`, `ent/postitem/*`, and related generated files
- Create: the timestamped `refine_post_publishing_state.sql` path printed by `make migration NAME=refine_post_publishing_state`
- Modify: `ent/migrate/migrations/atlas.sum`
- Modify: `ent/migrate/migrations_test.go`

**Interfaces:**
- Produces post statuses `deleting` and `deletion_failed`.
- Produces item fields `submission_state`, `submission_started_at`, `outcome_confirmed_at`, `outcome_confirmed_by`, and `confirmed_x_post_url`.
- Later repositories rely on `submission_state` values `not_started`, `submitting`, `published`, and `outcome_unknown`.

- [ ] **Step 1: Add failing schema descriptor tests**

```go
func TestPostItemSchemaCapturesAmbiguousSubmissionResolution(t *testing.T) {
    fields := fieldsByName((PostItem{}).Fields())
    state := fields["submission_state"]
    if state == nil || state.Default == nil {
        t.Fatal("submission_state must default to not_started")
    }
    for _, name := range []string{"submission_started_at", "outcome_confirmed_at", "outcome_confirmed_by", "confirmed_x_post_url"} {
        if fields[name] == nil || !fields[name].Optional || !fields[name].Nillable {
            t.Fatalf("%s must be nullable", name)
        }
    }
}

func TestPostSchemaIncludesDurableDeletionStates(t *testing.T) {
    status := fieldsByName((Post{}).Fields())["status"]
    if !slices.Contains(status.Enums, "deleting") || !slices.Contains(status.Enums, "deletion_failed") {
        t.Fatal("post status must include deleting and deletion_failed")
    }
}
```

- [ ] **Step 2: Run the focused schema tests and observe failure**

Run: `go test ./ent/schema -run 'Test(PostItemSchemaCapturesAmbiguousSubmissionResolution|PostSchemaIncludesDurableDeletionStates)' -v`

Expected: FAIL because the fields and deletion enum values do not exist.

- [ ] **Step 3: Add the minimal Ent fields and enum values**

```go
field.Enum("submission_state").
    Values("not_started", "submitting", "published", "outcome_unknown").
    Default("not_started"),
field.Time("submission_started_at").Optional().Nillable(),
field.Time("outcome_confirmed_at").Optional().Nillable(),
field.UUID("outcome_confirmed_by", uuid.UUID{}).Optional().Nillable(),
field.String("confirmed_x_post_url").Optional().Nillable().MaxLen(2048),
```

Add `deleting` and `deletion_failed` to the existing post status values. Keep confirmation data as an audit snapshot rather than an Ent edge so deleting a user cannot invalidate the record before aggregate cleanup.

- [ ] **Step 4: Regenerate Ent and create the Atlas migration**

Run: `go generate ./ent`

Run: `make migration NAME=refine_post_publishing_state`

Inspect the migration to ensure it adds only the intended columns and does not drop existing tables or indexes.

- [ ] **Step 5: Extend migration replay assertions and run validation**

Add column checks for the new fields to `ent/migrate/migrations_test.go` following the existing PostgreSQL catalog-query pattern.

Run: `go test ./ent/schema -v`

Run: `TEST_DATABASE_URL='postgres://infoai:infoai@localhost:5432/infoai_test?sslmode=disable' go test ./ent/migrate -v`

Expected: PASS.

- [ ] **Step 6: Report and wait for approval before commit**

Report schema changes, generated files, migration SQL, commands/output, and any unavailable database dependency. After explicit approval:

```bash
git add ent
git commit -m "feat: refine publishing recovery state"
```

---

### Task 2: Add replaceable error reporting and structured metrics

**Files:**
- Create: `internal/platform/errorreporting/reporter.go`
- Create: `internal/platform/errorreporting/file.go`
- Create: `internal/platform/errorreporting/file_test.go`
- Create: `internal/platform/metrics/recorder.go`
- Create: `internal/platform/metrics/slog.go`
- Create: `internal/platform/metrics/slog_test.go`
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Modify: `cmd/api/main.go`
- Modify: `cmd/worker/main.go`

**Interfaces:**
- Produces `errorreporting.Reporter` with `Capture(context.Context, error, Event)` and `Close() error`.
- Produces `errorreporting.Event` containing only allowlisted identifiers/classification fields.
- Produces `metrics.Recorder` with focused publication, queue, lease, media, and cleanup measurements emitted as structured `slog` events.
- Produces config fields `ErrorLogPath`, `ErrorLogMaxBytes`, and `ErrorLogRetainedFiles`.

- [ ] **Step 1: Write failing reporter tests**

```go
func TestFileReporterWritesOneRedactedJSONEventPerLine(t *testing.T) {
    path := filepath.Join(t.TempDir(), "errors.jsonl")
    reporter, err := NewFileReporter(FileConfig{Path: path, MaxBytes: 1 << 20, RetainedFiles: 2})
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { _ = reporter.Close() })

    reporter.Capture(context.Background(), errors.New("database unavailable"), Event{
        Code: "publication_failed", PostID: "post-1", JobID: 42,
    })
    if err := reporter.Close(); err != nil { t.Fatal(err) }

    data, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if !bytes.Contains(data, []byte(`"code":"publication_failed"`)) || bytes.Contains(data, []byte("token")) {
        t.Fatalf("unexpected event: %s", data)
    }
}
```

Add focused tests for `0600` permissions, size rotation, concurrent capture, close/flush, and a writer failure falling back without panicking.

- [ ] **Step 2: Run tests and observe failure**

Run: `go test ./internal/platform/errorreporting -v`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Define the port and allowlisted event**

```go
type Reporter interface {
    Capture(context.Context, error, Event)
    Close() error
}

type Event struct {
    Code          string `json:"code"`
    Classification string `json:"classification,omitempty"`
    RequestID     string `json:"request_id,omitempty"`
    PostID        string `json:"post_id,omitempty"`
    ItemID        string `json:"item_id,omitempty"`
    AttemptID     string `json:"attempt_id,omitempty"`
    JobID         int64  `json:"job_id,omitempty"`
    Worker        string `json:"worker,omitempty"`
    LeaseVersion  int64  `json:"lease_version,omitempty"`
    RetryNumber   int    `json:"retry_number,omitempty"`
    ProviderStatus int   `json:"provider_status,omitempty"`
}
```

The file adapter owns serialization, buffering, rotation, and stderr fallback. Services send IDs and classifications, never arbitrary maps.

Add a compile-time fake reporter test to prove services depend only on `Reporter`; a future `SentryReporter` can replace `FileReporter` at the composition root without changing application packages.

- [ ] **Step 4: Add configuration parsing tests and implementation**

Test defaults and invalid values:

```text
ERROR_LOG_PATH=var/log/infoai/errors.jsonl
ERROR_LOG_MAX_BYTES=10485760
ERROR_LOG_RETAINED_FILES=5
```

Require positive size/retention values and a non-empty path.

- [ ] **Step 5: Add focused structured metrics**

Define explicit methods rather than arbitrary metric names:

```go
type Recorder interface {
    PublicationFinished(context.Context, PublicationMeasurement)
    QueueLatency(context.Context, time.Duration)
    LeaseExpired(context.Context, string)
    MediaProcessingFinished(context.Context, MediaMeasurement)
    CleanupBacklog(context.Context, int)
}
```

Implement the initial adapter with `slog` structured events so the deployment log collector can aggregate success/failure, retry, ambiguity, duration, expired-lease, and backlog fields. Test exact event names and ensure measurements contain no user content.

- [ ] **Step 6: Compose and close the reporter in API and worker roots**

Construct the reporter after configuration and defer `Close`. Startup failures return from `run`; capture failures never fail a business operation. Pass the reporter into later dependency structs only after those consumers are introduced.

- [ ] **Step 7: Run focused validation**

Run: `go test ./internal/platform/errorreporting ./internal/platform/metrics ./internal/platform/config ./cmd/api ./cmd/worker -v`

Expected: PASS.

- [ ] **Step 8: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/platform/errorreporting internal/platform/metrics internal/platform/config cmd/api/main.go cmd/worker/main.go
git commit -m "feat: add publishing observability adapters"
```

---

### Task 3: Implement media policy and provider-neutral local storage

**Files:**
- Create: `internal/posts/media.go`
- Create: `internal/posts/media_test.go`
- Create: `internal/platform/mediastorage/storage.go`
- Create: `internal/platform/mediastorage/local.go`
- Create: `internal/platform/mediastorage/local_test.go`
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`

**Interfaces:**
- Produces `posts.MediaStorage`:
  `Put(context.Context, string, io.Reader) (posts.StoredObject, error)`,
  `Open(context.Context, string) (io.ReadCloser, error)`, and
  `Delete(context.Context, string) error`.
- Produces `posts.MediaPolicy.Validate(header []byte, size int64, siblings []MediaMetadata) (DetectedMedia, error)`.
- Local adapter consumes a configured absolute or project-resolved storage root but never leaks it outside the adapter.

- [ ] **Step 1: Write failing media-policy tests**

Cover signature detection, size boundaries, four-image limit, image/GIF/video exclusivity, and unsupported files:

```go
func TestMediaPolicyRejectsVideoBesideImage(t *testing.T) {
    policy := DefaultMediaPolicy()
    _, err := policy.Validate(mp4Header, 1024, []MediaMetadata{{Category: MediaImage}})
    if !errors.Is(err, ErrInvalidMediaCombination) {
        t.Fatalf("error = %v", err)
    }
}
```

- [ ] **Step 2: Run policy tests and observe failure**

Run: `go test ./internal/posts -run Media -v`

Expected: FAIL because the policy does not exist.

- [ ] **Step 3: Implement the minimal centralized media policy**

Use `http.DetectContentType` plus explicit signatures and MP4 `ftyp` inspection. Return a category, normalized MIME type, and size. Do not transcode or parse client-provided paths.

- [ ] **Step 4: Write failing local-storage tests**

```go
func TestLocalStoragePutIsAtomicAndReturnsChecksum(t *testing.T) {
    root := t.TempDir()
    store, err := NewLocal(root)
    if err != nil { t.Fatal(err) }
    object, err := store.Put(context.Background(), "ab/asset-id", strings.NewReader("media"))
    if err != nil { t.Fatal(err) }
    if object.Size != 5 || len(object.SHA256) != sha256.Size { t.Fatalf("object = %+v", object) }
}
```

Add traversal (`../x`), absolute-key, interrupted-reader, open, delete, missing-delete idempotency, and permission tests.

- [ ] **Step 5: Run storage tests and observe failure**

Run: `go test ./internal/platform/mediastorage -v`

Expected: FAIL because the adapter does not exist.

- [ ] **Step 6: Implement local streaming storage**

Validate each slash-separated key component, create directories with restrictive permissions, write a temporary file inside the destination directory, hash/count through `io.MultiWriter`, call `Sync`, close, and atomically rename. Return only key, size, and checksum.

- [ ] **Step 7: Add `MEDIA_STORAGE_ROOT` configuration and validate**

Default to `var/media` for development. Reject an empty configured value. Do not create directories inside `config.Load`; adapter construction owns filesystem effects.

- [ ] **Step 8: Run validation**

Run: `go test ./internal/posts ./internal/platform/mediastorage ./internal/platform/config -v`

Expected: PASS.

- [ ] **Step 9: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts internal/platform/mediastorage internal/platform/config
git commit -m "feat: add local media storage"
```

---

### Task 4: Implement post validation, repository, and draft use cases

**Files:**
- Create: `internal/posts/model.go`
- Create: `internal/posts/errors.go`
- Create: `internal/posts/service.go`
- Create: `internal/posts/service_test.go`
- Create: `internal/posts/repository.go`
- Create: `internal/posts/ent_repository.go`
- Create: `internal/posts/ent_repository_test.go`

**Interfaces:**
- Produces `posts.Service` methods `Create`, `Get`, `List`, `Update`, `Schedule`, and `CancelSchedule`.
- Produces owner-scoped `posts.Repository` methods used by HTTP and later workers.
- Consumes `MediaPolicy`, `Clock`, and `PublicationQueue`; scheduling queue integration is completed in Task 6.

- [ ] **Step 1: Define domain commands and write failing service tests**

```go
type CreateCommand struct {
    OwnerID uuid.UUID
    XAccountID uuid.UUID
    CreationMode CreationMode
    Items []ItemInput
}

type ItemInput struct {
    Text string
}
```

Tests cover one-to-25 ordered items, empty items, owner/account mismatch, cached subscription limits, immutable states, UTC scheduling, rescheduling, and cancellation returning `scheduled` to `draft`.

- [ ] **Step 2: Run service tests and observe failure**

Run: `go test ./internal/posts -run 'TestService' -v`

Expected: FAIL because commands/service are undefined.

- [ ] **Step 3: Implement minimal domain validation and service orchestration**

Define stable sentinel/typed errors such as `ErrNotFound`, `ErrNotEditable`, `ErrInvalidTransition`, and field validation errors. Keep HTTP status mapping outside this package.

Use an X-compatible weighted-text counter behind:

```go
type TextPolicy interface {
    Validate(text, subscriptionType string) error
}
```

Pin and test any third-party weighted-count dependency before use; if no maintained Go library matches X rules, implement a focused counter from X's published weighting configuration and lock behavior with URL/emoji tests.

- [ ] **Step 4: Write failing Ent repository tests**

Test owner-scoped get/list, atomic aggregate creation, stable ordering, replacement of editable items, account ownership, and conditional transitions. Use `TEST_DATABASE_URL` and clean each created owner aggregate.

- [ ] **Step 5: Implement the Ent adapter**

Keep transaction boundaries in repository methods that modify the aggregate. Return domain models rather than exposing Ent entities to handlers/services.

- [ ] **Step 6: Run validation**

Run: `go test ./internal/posts -v`

Expected: unit tests PASS; database tests PASS when `TEST_DATABASE_URL` is available or report their explicit skip.

- [ ] **Step 7: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts
git commit -m "feat: add post draft service"
```

---

### Task 5: Add authenticated post CRUD and media HTTP endpoints

**Files:**
- Create: `internal/httpapi/post_handlers.go`
- Create: `internal/httpapi/post_handlers_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/router_test.go`
- Modify: `internal/httpapi/response.go`
- Modify: `internal/posts/service.go`
- Modify: `internal/posts/ent_repository.go`

**Interfaces:**
- Consumes Task 4 post-service commands and Task 3 `MediaStorage`.
- Produces authenticated JSON endpoints for draft CRUD/schedule/cancel and multipart media upload/removal.
- Adds `Posts PostService` and `Media MediaStorage` to `httpapi.Dependencies`.

- [ ] **Step 1: Write failing route/handler tests**

Use fake services and session cookies. Assert authentication, ownership delegation, unknown-field rejection, `PATCH` CORS allowance, stable error codes, ordered response items, multipart limits, and schedule timestamps with offsets.

```go
func TestCreatePostRequiresAuthentication(t *testing.T) {
    response := request(t, handler, http.MethodPost, "/api/v1/posts", createBody, nil)
    if response.Code != http.StatusUnauthorized { t.Fatalf("status = %d", response.Code) }
}
```

- [ ] **Step 2: Run focused tests and observe failure**

Run: `go test ./internal/httpapi -run Post -v`

Expected: FAIL because routes and dependency are absent.

- [ ] **Step 3: Add handler interfaces, request/response DTOs, and routes**

Handlers perform decoding, path UUID parsing, multipart bounding, service calls, and error mapping only. Add `PATCH` to CORS methods. Do not expose Ent objects.

- [ ] **Step 4: Implement media upload consistency**

Generate the opaque key in the application service, call `MediaStorage.Put`, then persist metadata. If persistence fails, call `Delete`; if that fails, insert `StorageDeletion` through the repository.

- [ ] **Step 5: Run validation**

Run: `go test ./internal/httpapi ./internal/posts -v`

Expected: PASS.

- [ ] **Step 6: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/httpapi internal/posts
git commit -m "feat: add post draft API"
```

---

### Task 6: Add typed River jobs and transactional publication queueing

**Files:**
- Create: `internal/posts/jobs.go`
- Create: `internal/posts/jobs_test.go`
- Create: `internal/posts/river_queue.go`
- Create: `internal/posts/river_queue_test.go`
- Modify: `internal/posts/service.go`
- Modify: `internal/posts/service_test.go`
- Modify: `internal/httpapi/post_handlers.go`
- Modify: `internal/httpapi/post_handlers_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `internal/platform/riverqueue/client.go`
- Modify: `internal/platform/riverqueue/client_test.go`

**Interfaces:**
- Produces `PublicationQueue` methods `Publish`, `DeletePublished`, and `CleanupStorage` accepting `*sql.Tx` so job insertion shares the state transaction.
- Produces River args `PublishArgs`, `DeletePostArgs`, `RecoverArgs`, and `StorageCleanupArgs` with stable IDs and trigger/version fields.

- [ ] **Step 1: Write failing job-kind and insert-option tests**

```go
func TestPublishArgsRouteToPublishQueue(t *testing.T) {
    args := PublishArgs{PostID: uuid.New(), Trigger: TriggerScheduled, StateVersion: 3}
    if args.Kind() != "publish_post" { t.Fatalf("kind = %q", args.Kind()) }
    options := args.InsertOpts()
    if options.Queue != riverqueue.PublishQueue { t.Fatalf("queue = %q", options.Queue) }
}
```

Test scheduled `ScheduledAt`, active-job state-version rejection of duplicate deliveries, and maintenance routing. Do not depend on River uniqueness for correctness.

- [ ] **Step 2: Run tests and observe failure**

Run: `go test ./internal/posts -run 'Test(PublishArgs|RiverQueue)' -v`

Expected: FAIL because job args/adapter do not exist.

- [ ] **Step 3: Implement job args and River adapter**

Use River's transaction-bound client/insert API with the `*sql.Tx` passed by `database.WithTx`. Payloads contain IDs and intent only, never text or media metadata.

- [ ] **Step 4: Add failing service tests for atomic publish/schedule/cancel**

Assert state changes and job insertion occur in the same fake transaction contract, active job identity/version is stored, and stale cancellation invalidates the version.

- [ ] **Step 5: Implement publish/schedule queue orchestration**

Immediate publish transitions `draft → publishing`; scheduling transitions `draft|scheduled → scheduled`; cancellation transitions `scheduled → draft`. Each update is conditional on owner/current state/version.

- [ ] **Step 6: Add the immediate-publish HTTP command**

Register `POST /api/v1/posts/{postID}/publish`. Test `202 Accepted`, owner scoping, the `draft` source-state requirement, duplicate request conflict, and returned current job state.

- [ ] **Step 7: Run validation**

Run: `go test ./internal/posts ./internal/platform/riverqueue ./internal/httpapi -v`

Expected: PASS.

- [ ] **Step 8: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts internal/platform/riverqueue internal/httpapi
git commit -m "feat: queue post publication transactionally"
```

---

### Task 7: Extend the X adapter for media, creation, deletion, and failure classification

**Files:**
- Create: `internal/integrations/x/publisher.go`
- Create: `internal/integrations/x/publisher_test.go`
- Create: `internal/integrations/x/errors.go`
- Create: `internal/integrations/x/errors_test.go`
- Modify: `internal/integrations/x/client.go`

**Interfaces:**
- Produces `posts.XPublisher`:
  `UploadMedia(context.Context, string, posts.MediaUpload) (string, error)`,
  `CreatePost(context.Context, string, posts.XPostInput) (string, error)`, and
  `DeletePost(context.Context, string, string) error`.
- Produces classified errors: `DefiniteRetryable`, `Permanent`, `Reauthorization`, and `Ambiguous`.

- [ ] **Step 1: Write failing transport classification tests**

Use a custom `RoundTripper` and `httptrace.ClientTrace` signals to distinguish failure before request write from failure after `WroteRequest`.

```go
func TestCreatePostClassifiesLostResponseAfterWriteAsAmbiguous(t *testing.T) {
    client := newTestPublisher(roundTripFunc(func(r *http.Request) (*http.Response, error) {
        return nil, io.ErrUnexpectedEOF
    }))
    _, err := client.CreatePost(context.Background(), "token", posts.XPostInput{Text: "hello"})
    if ClassificationOf(err) != Ambiguous { t.Fatalf("classification = %v", ClassificationOf(err)) }
}
```

The fake transport must explicitly trigger the trace callback so the test models transmitted bytes rather than assuming every transport error is ambiguous.

- [ ] **Step 2: Write failing HTTP contract tests**

Cover X v2 INIT/APPEND/FINALIZE/STATUS, bounded chunks, `tweet_image|tweet_gif|tweet_video`, alt metadata, post/reply JSON, 201 response parsing, DELETE, 404-as-deleted, rate-limit headers, 401/403 reauthorization, 429/5xx retryability, malformed success ambiguity, and body size limits.

- [ ] **Step 3: Run tests and observe failure**

Run: `go test ./internal/integrations/x -run 'Test(Publisher|CreatePost|UploadMedia|DeletePost)' -v`

Expected: FAIL because publishing operations do not exist.

- [ ] **Step 4: Implement centralized X request execution and classification**

One helper owns bounded response reads, safe error extraction, rate-limit parsing, and classification. Never include the access token or unfiltered body in returned errors.

- [ ] **Step 5: Implement chunked upload, processing wait, create/reply, and delete**

Honor X `check_after_secs` within a context deadline. Media STATUS polling is allowed; do not implement post lookup, timeline, or search methods.

- [ ] **Step 6: Run validation**

Run: `go test ./internal/integrations/x -v`

Expected: PASS with no network access.

- [ ] **Step 7: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/integrations/x
git commit -m "feat: add X publishing adapter"
```

---

### Task 8: Implement leased ordered publishing and bounded retries

**Files:**
- Create: `internal/posts/publisher.go`
- Create: `internal/posts/publisher_test.go`
- Create: `internal/posts/retry.go`
- Create: `internal/posts/retry_test.go`
- Modify: `internal/posts/repository.go`
- Modify: `internal/posts/ent_repository.go`
- Modify: `internal/posts/ent_repository_test.go`
- Modify: `internal/posts/service.go`
- Modify: `internal/posts/service_test.go`
- Modify: `internal/httpapi/post_handlers.go`
- Modify: `internal/httpapi/post_handlers_test.go`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces `Publisher.Publish(context.Context, PublishCommand) error`.
- Consumes `PostRepository`, existing X token service through `AccessToken`, `XPublisher`, `MediaStorage`, `PublicationQueue`, `Clock`, `Jitter`, and `ErrorReporter`.
- Repository produces conditional `ClaimLease`, `BeginAttempt`, `MarkSubmitting`, `MarkItemPublished`, `ScheduleRetry`, `MarkFailed`, `MarkOutcomeUnknown`, and `MarkPublished` operations.

- [ ] **Step 1: Write failing retry-policy tests**

```go
func TestRetryPolicyStopsAfterThreeAttempts(t *testing.T) {
    policy := RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{30 * time.Second, 2 * time.Minute}, Jitter: fixedJitter(1)}
    if _, ok := policy.Next(3, time.Time{}); ok { t.Fatal("third failed attempt must stop") }
}
```

Test delay bounds, rate-limit reset overriding a shorter delay, and a manual cycle resetting the count.

- [ ] **Step 2: Write failing publishing-service tests**

Cover single success, ordered replies, media before creation, skipping known X IDs, partial resume, definite retry scheduling, permanent failure, reauthorization, ambiguous error, and lease loss.

- [ ] **Step 3: Run tests and observe failure**

Run: `go test ./internal/posts -run 'Test(Publisher|RetryPolicy)' -v`

Expected: FAIL because publisher/retry policy do not exist.

- [ ] **Step 4: Implement the minimal publishing workflow**

Before each X create, persist `submitting`. After success, immediately persist X ID and `published`. Do no unrelated work between response parsing and persistence. When earlier items have IDs, pass the previous ID as `ReplyToID`.

- [ ] **Step 5: Implement conditional Ent lease/attempt transitions**

Every worker write predicates on post ID, lease token, and lease version. A zero-row update returns `ErrLeaseLost`; it must not be retried by the stale worker.

- [ ] **Step 6: Wire error reporting at service boundaries**

Capture actionable failures using only allowlisted IDs/classification. Do not report expected validation conflicts as operational errors.

- [ ] **Step 7: Add manual retry service and HTTP command**

Implement `Retry(context.Context, RetryCommand) error` for `failed` and `partially_published` definite failures. Reset the automatic cycle count, preserve publication attempts, enqueue transactionally, and reject `outcome_unknown` until the user resolves it. Register `POST /api/v1/posts/{postID}/retry` and test `202`, `404`, `409`, and ownership behavior.

- [ ] **Step 8: Run validation**

Run: `go test ./internal/posts ./internal/httpapi -v`

Expected: PASS; database tests PASS with `TEST_DATABASE_URL` or explicitly skip.

- [ ] **Step 9: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts internal/httpapi
git commit -m "feat: publish posts and threads durably"
```

---

### Task 9: Register publish and PostgreSQL-only recovery workers

**Files:**
- Create: `internal/posts/workers.go`
- Create: `internal/posts/workers_test.go`
- Modify: `cmd/worker/main.go`
- Modify: `cmd/worker/main_test.go`
- Modify: `internal/platform/riverqueue/client.go`

**Interfaces:**
- Consumes Task 6 River args and Task 8 `Publisher`.
- Produces River workers for publication and periodic PostgreSQL recovery.
- Recovery repository operations claim expired leases, due retries, and due schedules conditionally.

- [ ] **Step 1: Write failing worker registration and delegation tests**

Assert all args kinds have registered workers and publish workers pass River job ID/attempt metadata into `PublishCommand`.

- [ ] **Step 2: Write failing recovery tests**

Cover:

- expired lease with item `submitting` → `outcome_unknown`, no queue insert;
- expired lease before submission → recovery publication job;
- due retry → one conditional claim and one job;
- due scheduled post → one conditional claim and one job;
- two concurrent scans → only one successful claim.

- [ ] **Step 3: Run tests and observe failure**

Run: `go test ./internal/posts ./cmd/worker -run 'Test.*(Worker|Recovery)' -v`

Expected: FAIL because workers are not registered.

- [ ] **Step 4: Implement worker adapters and recovery batches**

Workers are thin adapters. Recovery uses bounded batches and keyset ordering; it reads PostgreSQL only and never calls X post-read endpoints.

- [ ] **Step 5: Compose worker dependencies**

Construct Ent repository, local storage, X publisher, token service, queue, reporter, and publisher in `cmd/worker/main.go`; register workers before constructing the River client.

- [ ] **Step 6: Run validation**

Run: `go test ./internal/posts ./cmd/worker ./internal/platform/riverqueue -v`

Expected: PASS.

- [ ] **Step 7: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts cmd/worker/main.go cmd/worker/main_test.go internal/platform/riverqueue
git commit -m "feat: run publishing recovery workers"
```

---

### Task 10: Add manual ambiguous-outcome resolution

**Files:**
- Modify: `internal/posts/service.go`
- Modify: `internal/posts/service_test.go`
- Modify: `internal/posts/repository.go`
- Modify: `internal/posts/ent_repository.go`
- Modify: `internal/posts/ent_repository_test.go`
- Modify: `internal/httpapi/post_handlers.go`
- Modify: `internal/httpapi/post_handlers_test.go`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces `ResolveOutcome(context.Context, ResolveOutcomeCommand) (Post, error)`.
- `ResolveOutcomeCommand` carries owner/post/item IDs, decision `not_published|published|unresolved`, and an optional X URL required for `published`.

- [ ] **Step 1: Write failing service tests for all decisions**

```go
func TestResolvePublishedOutcomeAttachesUserSuppliedXID(t *testing.T) {
    command := ResolveOutcomeCommand{OwnerID: owner, PostID: post, ItemID: item, Decision: OutcomePublished, XURL: "https://x.com/person/status/1234567890"}
    got, err := service.ResolveOutcome(ctx, command)
    if err != nil { t.Fatal(err) }
    if got.Items[0].XPostID != "1234567890" { t.Fatalf("XPostID = %q", got.Items[0].XPostID) }
}
```

Test accepted `x.com` and `twitter.com` hosts, HTTPS requirement, numeric IDs, local uniqueness, owner scope, wrong item state, audit fields, single/final publication completion, partial continuation, retry-cycle reset, and unresolved no-op.

- [ ] **Step 2: Run tests and observe failure**

Run: `go test ./internal/posts -run ResolveOutcome -v`

Expected: FAIL because the command is absent.

- [ ] **Step 3: Implement URL parsing and atomic resolution**

Do not call X. Persist actor/time/URL, attach the extracted ID when confirmed, and enqueue the next publication job transactionally when a partial thread can continue.

- [ ] **Step 4: Write failing HTTP tests and add the command endpoint**

Use `POST /api/v1/posts/{postID}/items/{itemID}/resolve-outcome`. Map invalid attestation to `422`, ownership/not-found to `404`, and state conflict to `409`.

- [ ] **Step 5: Run validation**

Run: `go test ./internal/posts ./internal/httpapi -v`

Expected: PASS and no X client invocation in any resolution test.

- [ ] **Step 6: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts internal/httpapi
git commit -m "feat: resolve ambiguous publication outcomes"
```

---

### Task 11: Implement confirmed deletion from X and local cleanup

**Files:**
- Create: `internal/posts/deleter.go`
- Create: `internal/posts/deleter_test.go`
- Create: `internal/posts/cleanup.go`
- Create: `internal/posts/cleanup_test.go`
- Modify: `internal/posts/service.go`
- Modify: `internal/posts/service_test.go`
- Modify: `internal/posts/repository.go`
- Modify: `internal/posts/ent_repository.go`
- Modify: `internal/posts/workers.go`
- Modify: `internal/httpapi/post_handlers.go`
- Modify: `internal/httpapi/post_handlers_test.go`

**Interfaces:**
- Produces `RequestDeletion(context.Context, DeleteCommand) error` requiring `ConfirmXDeletion` when any known X ID exists.
- Produces `Deleter.Delete(context.Context, DeleteJobCommand) error` and `Cleanup.Run(context.Context, StorageCleanupArgs) error`.
- Consumes `XPublisher.DeletePost`, `MediaStorage.Delete`, repository, queue, token service, retry policy, and reporter.

- [ ] **Step 1: Write failing deletion-request tests**

Cover draft/scheduled local deletion, scheduled job invalidation, confirmation required for known X IDs, duplicate request idempotency, and rejection while active publishing owns a valid lease.

- [ ] **Step 2: Write failing deletion-worker tests**

Cover reverse thread order, progress persistence after each ID, 404 success, definite retry, reauthorization, exhausted `deletion_failed`, and final storage-deletion records before aggregate removal.

- [ ] **Step 3: Write failing cleanup-worker tests**

Cover successful file deletion, missing file success, retry scheduling, exhausted record retention, and bounded batches.

- [ ] **Step 4: Run tests and observe failure**

Run: `go test ./internal/posts -run 'Test(RequestDeletion|Deleter|Cleanup)' -v`

Expected: FAIL because deletion services are absent.

- [ ] **Step 5: Implement deletion and cleanup services**

Delete known X IDs from highest item position to lowest. Clear/record each completed X ID before continuing. Only after all known IDs converge to deleted should the repository insert storage cleanup work and remove/tombstone the aggregate.

- [ ] **Step 6: Register deletion and cleanup River workers**

Use the maintenance queue and focused worker adapters. Worker failures are classified and reported without leaking provider bodies.

- [ ] **Step 7: Add HTTP confirmation contract**

Require `{"confirm_x_deletion":true}` for aggregates with known X content. Return `202 Accepted` for asynchronous X deletion and `204 No Content` for completed local-only deletion.

- [ ] **Step 8: Run validation**

Run: `go test ./internal/posts ./internal/httpapi ./cmd/worker -v`

Expected: PASS.

- [ ] **Step 9: Report and wait for approval before commit**

After explicit approval:

```bash
git add internal/posts internal/httpapi cmd/worker
git commit -m "feat: delete published posts and media"
```

---

### Task 12: Complete composition, integration coverage, and operations documentation

**Files:**
- Modify: `cmd/api/main.go`
- Create: `cmd/api/main_test.go`
- Modify: `cmd/worker/main.go`
- Modify: `cmd/worker/main_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/router_test.go`
- Create: `internal/posts/integration_test.go`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `.env.example`

**Interfaces:**
- Consumes every preceding task and produces the runnable API/worker feature.
- No new domain abstraction is introduced in this task.

- [ ] **Step 1: Write failing end-to-end integration tests with fakes**

Cover immediate single publication, scheduled thread, partial resume, retry exhaustion, ambiguous user confirmation, stale jobs, confirmed X deletion, and durable storage cleanup. Use PostgreSQL/River plus local HTTP X and temporary filesystem adapters; never use X network endpoints.

- [ ] **Step 2: Run integration tests and observe the first missing composition behavior**

Run: `TEST_DATABASE_URL='postgres://infoai:infoai@localhost:5432/infoai_test?sslmode=disable' go test ./internal/posts -run Integration -v`

Expected: FAIL until API/worker dependency wiring and recurring recovery insertion are complete.

- [ ] **Step 3: Finish API and worker composition**

Wire shared config, Ent repositories, transactional River adapter, local storage, X publisher, post/publishing/deletion/cleanup services, and file reporter. Ensure shutdown order stops HTTP/River before closing reporter, storage resources, and database connections.

- [ ] **Step 4: Add operational commands and documentation**

Document:

- `MEDIA_STORAGE_ROOT` and error-reporting variables.
- Running `cmd/api` and `cmd/worker` together.
- Atlas and River migrations.
- Error-log rotation/permissions.
- No-read ambiguity recovery behavior.
- Required X scopes and supported media constraints.
- The frontend's irreversible X-deletion warning and user confirmation flow.

Add a `run-worker` Make target without replacing the existing `run` API target.

- [ ] **Step 5: Run full verification**

Run: `gofmt -w` on changed non-generated Go files.

Run: `go generate ./ent && git diff --exit-code -- ent`

Run: `go test ./...`

Run: `TEST_DATABASE_URL='postgres://infoai:infoai@localhost:5432/infoai_test?sslmode=disable' go test ./...`

Run: `go vet ./...`

Run: `go build ./cmd/api ./cmd/worker`

Run: `git diff --check`

Expected: all commands PASS; Ent regeneration leaves no diff.

- [ ] **Step 6: Review against completion criteria**

Confirm every completion criterion in the spec has a passing unit, adapter, or integration test. Confirm no X post-read endpoint exists with:

Run: `rg -n '/2/(tweets(/search)?|users/.*/tweets)' internal cmd`

Expected: only `POST /2/tweets` creation references; no GET lookup/timeline/search implementation.

- [ ] **Step 7: Report and wait for approval before commit**

Report all changed files, exact verification output, skipped environmental checks, and remaining operational risks. After explicit approval:

```bash
git add cmd internal Makefile README.md .env.example
git commit -m "feat: complete post publishing workflow"
```

Do not include `.env.example` in the command if that file does not exist or was not changed.

---

## Execution Notes

- Execute tasks in order because later interfaces depend on earlier tasks.
- Treat each task as its own review gate. Never begin the next task or commit the current one until the user approves the completed task.
- Generated Ent output belongs in the same review/commit as its schema change.
- If X documentation changes during implementation, update the centralized policy/adapter tests and present the design impact before broadening scope.
- If an implementation task reveals that an interface needs to widen, stop and review the change rather than adding speculative methods.
