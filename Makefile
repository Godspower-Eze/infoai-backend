.PHONY: build test test-db test-db-migrate generate migration migrate river-migrate run run-worker

GODOTENV_RUN := go run github.com/joho/godotenv/cmd/godotenv
TEST_DATABASE_URL ?= postgres://infoai:infoai@localhost:5432/infoai_test?sslmode=disable

define run_with_dotenv
	@if [ -f .env ]; then \
		$(GODOTENV_RUN) -f .env $(1); \
	else \
		$(1); \
	fi
endef

build:
	go build ./cmd/api
	go build ./cmd/worker

test:
	go test ./...

test-db:
	docker compose up -d --wait postgres
	docker compose exec -T postgres sh -c 'psql -U "$$POSTGRES_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '\''infoai_test'\''" | grep -q 1 || createdb -U "$$POSTGRES_USER" infoai_test'

test-db-migrate: test-db
	DATABASE_URL="$(TEST_DATABASE_URL)" atlas migrate apply --env local
	go run github.com/riverqueue/river/cmd/river@v0.41.1 migrate-up --database-url "$(TEST_DATABASE_URL)"

generate:
	go generate ./ent

migration:
	@test -n "$(NAME)" || (echo "usage: make migration NAME=describe_change"; exit 1)
	$(call run_with_dotenv,go run ./ent/migrate/generate.go $(NAME))

migrate:
	$(call run_with_dotenv,atlas migrate apply --env local)

river-migrate:
	$(call run_with_dotenv,sh -c 'go run github.com/riverqueue/river/cmd/river@v0.41.1 migrate-up --database-url "$$DATABASE_URL"')

run:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker
