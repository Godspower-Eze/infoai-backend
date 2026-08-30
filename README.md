# InfoAI backend

Go backend for authentication, X account connections, post drafts, media, scheduling, publishing, retries, recovery, and deletion.

## Architecture

```text
cmd/api                  HTTP API composition and server
cmd/worker               River worker composition and lifecycle
internal/httpapi         Routes, handlers, middleware, and response DTOs
internal/posts           Post domain, services, ports, persistence, and workers
internal/integrations/x  OAuth, tokens, and X publishing adapter
internal/platform        Database, River, storage, logging, metrics, and config
ent                      Schema, generated ORM, and Atlas migrations
```

HTTP handlers call application services through focused interfaces. Services depend on ports implemented by Ent, River, the filesystem, and X adapters.

## Local setup

Requirements: Go 1.26, PostgreSQL, and Atlas.

```sh
cp .env.example .env
openssl rand -base64 32
```

Put the generated value in `TOKEN_ENCRYPTION_KEY`, configure the X client values, then run:

```sh
docker compose up -d --wait postgres
make migrate
make river-migrate
```

Start the API and worker in separate terminals:

```sh
make run
make run-worker
```

The API defaults to `http://localhost:8080`.

```text
GET /health/live
GET /health/ready
```

## Routes

All `/api/v1/posts` and X account routes require the `infoai_session` cookie.

### Authentication

```text
POST /api/v1/auth/signup
POST /api/v1/auth/signin
POST /api/v1/auth/signout
GET  /api/v1/auth/me
```

### X accounts

```text
POST   /api/v1/integrations/x/authorize
GET    /api/v1/integrations/x/callback
GET    /api/v1/integrations/x/accounts
DELETE /api/v1/integrations/x/accounts/{accountID}
```

### Posts

```text
POST   /api/v1/posts
GET    /api/v1/posts
GET    /api/v1/posts/{postID}
PATCH  /api/v1/posts/{postID}
DELETE /api/v1/posts/{postID}

POST   /api/v1/posts/{postID}/publish
POST   /api/v1/posts/{postID}/retry
POST   /api/v1/posts/{postID}/schedule
POST   /api/v1/posts/{postID}/schedule/cancel

POST   /api/v1/posts/{postID}/items/{itemID}/media
DELETE /api/v1/posts/{postID}/items/{itemID}/media/{mediaID}
POST   /api/v1/posts/{postID}/items/{itemID}/resolve-outcome
```

Unsafe browser requests must carry the exact configured `Origin`.

## Publishing behavior

- Draft items may start empty so media can be attached before text is added. Publishing and scheduling require text or media on every item.
- Images support JPEG, PNG, and WebP up to 5 MiB, with at most four per item. A GIF up to 15 MiB or MP4 up to 512 MiB must be the item's only attachment.
- Retryable failures use bounded backoff and jitter. PostgreSQL-backed recovery handles due schedules, retries, expired leases, and cleanup.
- If X may have created a post but the response was lost, the item becomes `outcome_unknown`. The user must check X and confirm the result; the backend does not perform a paid post read.
- Deleting known X posts requires `{"confirm_x_deletion":true}`. The worker deletes threads from last item to first before local media cleanup.

Required X scopes: `tweet.read`, `users.read`, `tweet.write`, and `offline.access`.

## Operations

- `MEDIA_STORAGE_ROOT` defaults to `var/media`. Storage is behind `MediaStorage`, so another adapter can replace the filesystem.
- `ERROR_LOG_PATH` defaults to `var/log/infoai/errors.jsonl`. Logs use JSONL, `0600` permissions, size rotation, and configured retention.
- Set `TRUSTED_PROXY_CIDRS` only to trusted proxy ranges. When empty, rate limiting uses the direct TCP peer and ignores forwarding headers.
- Use absolute storage and log paths with correctly owned directories in production.

## Schema changes

Define schema changes under `ent/schema`, regenerate Ent, create an Atlas migration, and review the generated SQL:

```sh
go generate ./ent
ATLAS_DEV_DATABASE_URL='postgres://infoai:infoai@localhost:5432/infoai_dev?sslmode=disable' \
  make migration NAME=describe_change
```

Apply application and River migrations before starting services:

```sh
make migrate
make river-migrate
```

Application startup does not mutate the database schema.
