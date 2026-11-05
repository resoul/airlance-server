FLATC ?= flatc
SCHEMA_SOURCE ?= ../Airlance/submodules/Protocol/Schema

.PHONY: check test test-int build schema migrate-create
check: ## Vet and unit tests
	go vet ./...
	go test -race -count=1 ./...

test-int: ## Against real services: AIRLANCE_SERVER_TEST_DATABASE_URL, _S3_* and _MEILI_* must be set
	go test -race -count=1 -tags=integration ./internal/repository/... ./internal/infrastructure/database/... ./internal/infrastructure/objectstore/... ./internal/infrastructure/meili/...

migrate-create: ## A pair of empty migration files: make migrate-create name=add_things
	go run ./cmd/airlance-server migrate create $(name)

build:
	go build -o bin/airlance-server ./cmd/airlance-server

schema: ## Copy the schema from the app and regenerate. FLATC must be the version the app builds (25.12.19)
	cp $(SCHEMA_SOURCE)/*.fbs schema/
	cp ../Airlance/submodules/WireAuth/SPEC.md internal/transport/wireauth/SPEC.md
	$(FLATC) --go --go-namespace wire -o internal/transport schema/Envelope.fbs schema/Service.fbs
