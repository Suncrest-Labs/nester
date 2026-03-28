.PHONY: migrate-up migrate-down migrate-create

DATABASE_DSN ?= postgres://postgres:postgres@localhost:5432/nester?sslmode=disable

migrate-up:
	DATABASE_DSN="$(DATABASE_DSN)" go run ./internal/db/migrate.go up

migrate-down:
	@if [ -z "$(steps)" ]; then \
		echo "Usage: make migrate-down steps=<number_of_steps>"; \
		exit 1; \
	fi
	STEPS="$(steps)" DATABASE_DSN="$(DATABASE_DSN)" go run ./internal/db/migrate.go down

migrate-create:
	@if [ -z "$(name)" ]; then \
		echo "Usage: make migrate-create name=<migration_name>"; \
		exit 1; \
	fi
	NAME="$(name)" DATABASE_DSN="$(DATABASE_DSN)" go run ./internal/db/migrate.go create
