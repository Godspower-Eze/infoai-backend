# InfoAI backend

Go API for email/password authentication and connecting X accounts through OAuth 2.0 with PKCE. `cmd/api` is the executable composition root; domain and infrastructure packages remain importable and testable without starting a server.

## Stack

- Chi HTTP router
- Ent ORM and Atlas versioned migrations
- PostgreSQL through pgx
- SCS database-backed cookie sessions
- Argon2id password hashing
- AES-256-GCM encryption for X access and refresh tokens

## Local setup

Requirements: Go 1.26, PostgreSQL (or Podman/Docker Compose), and the Atlas CLI for applying or generating migrations.

1. Copy `.env.example` to `.env`, generate `TOKEN_ENCRYPTION_KEY` with `openssl rand -base64 32`, and set the X OAuth client values.
2. Start PostgreSQL with `podman compose up -d` or `docker compose up -d`.
3. Apply migrations with `make migrate`.
4. Start the service with `make run`.

`cmd/api` automatically loads `.env` from its current working directory. Variables already provided by your shell, container, CI system, or deployment platform take precedence over values in the file. A missing `.env` is allowed; a malformed or unreadable file stops startup.

The API listens on `HTTP_ADDRESS` (default `:8080`). Liveness is at `GET /health/live`; readiness is at `GET /health/ready`.

### Client IP trust model

By default, `TRUSTED_PROXY_CIDRS` is empty: the API uses the direct TCP peer for rate limiting and ignores forwarding headers. If the API runs behind proxies or a load balancer, set it to their comma-separated CIDR ranges, for example `10.0.0.0/8,2001:db8::/32`. In that mode the API resolves `X-Forwarded-For` from right to left, skipping trusted proxy hops. The API must then be network-restricted so clients cannot bypass those proxies, and each proxy must append or overwrite `X-Forwarded-For` correctly. IPv6 clients are rate-limited by canonical `/64` prefix.

## Schema changes

Define data in `ent/schema`, then run:

```sh
go generate ./ent
ATLAS_DEV_DATABASE_URL='postgres://infoai:infoai@localhost:5432/infoai_dev?sslmode=disable' make migration NAME=describe_change
```

Ent updates the ORM client. The migration generator compares the Ent schema against a clean development database and writes a timestamp-ordered SQL migration plus an updated `atlas.sum`. Review generated migrations and replay them in order with `make migrate`; application startup intentionally does not mutate the schema.

## Initial routes

- `POST /api/v1/auth/signup`
- `POST /api/v1/auth/signin`
- `POST /api/v1/auth/signout`
- `GET /api/v1/auth/me`
- `POST /api/v1/integrations/x/authorize`
- `GET /api/v1/integrations/x/callback`
- `GET /api/v1/integrations/x/accounts`
- `DELETE /api/v1/integrations/x/accounts/{accountID}`

All unsafe browser requests must carry the configured exact `Origin`. X routes require the `infoai_session` cookie. Only opaque, hashed session identifiers are stored by SCS; X OAuth state and PKCE data live in the session and X tokens are encrypted before persistence.
