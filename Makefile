.PHONY: build test generate migration migrate run

build:
	go build ./cmd/api

test:
	go test ./...

generate:
	go generate ./ent

migration:
	@test -n "$(NAME)" || (echo "usage: make migration NAME=describe_change"; exit 1)
	go run ./ent/migrate/generate.go $(NAME)

migrate:
	atlas migrate apply --env local

run:
	go run ./cmd/api
