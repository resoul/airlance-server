FLATC ?= flatc
SCHEMA_SOURCE ?= ../Airlance/submodules/Protocol/Schema

.PHONY: check test test-int build schema
check: ## Vet and unit tests
	go vet ./...
	go test -race -count=1 ./...

test-int: ## Against PostgreSQL: AIRLANCE_SERVER_TEST_DATABASE_URL must be set
	go test -race -count=1 -tags=integration ./internal/repository/...

build:
	go build -o bin/airlance-server ./cmd/airlance-server

schema: ## Copy the schema from the app and regenerate. FLATC must be the version the app builds (25.12.19)
	cp $(SCHEMA_SOURCE)/*.fbs schema/
	$(FLATC) --go --go-namespace wire -o internal/transport schema/Envelope.fbs schema/Service.fbs
