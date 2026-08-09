.PHONY: build test generate migration migrate run

GODOTENV_RUN := go run github.com/joho/godotenv/cmd/godotenv

define run_with_dotenv
	@if [ -f .env ]; then \
		$(GODOTENV_RUN) -f .env $(1); \
	else \
		$(1); \
	fi
endef

build:
	go build ./cmd/api

test:
	go test ./...

generate:
	go generate ./ent

migration:
	@test -n "$(NAME)" || (echo "usage: make migration NAME=describe_change"; exit 1)
	$(call run_with_dotenv,go run ./ent/migrate/generate.go $(NAME))

migrate:
	$(call run_with_dotenv,atlas migrate apply --env local)

run:
	go run ./cmd/api
