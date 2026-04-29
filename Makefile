build_dir := ./.build
IMAGE_NAME ?= "mockzilla/mockzilla"
VOLUME_NAME ?= "mockzilla"
VERSION ?= "latest"
GO_VERSION := $(shell awk '/^go / {print $$2}' go.mod)
PACKAGE := github.com/mockzilla/mockzilla/v2
GO_BUILD_FLAGS ?=

MIN_COVERAGE = 90

define docker-cmd
	sh -c 'docker-compose --env-file=.env.dist run --rm -e app_env=testing app $(1)'
endef

.PHONY: clean
clean:
	rm -rf ${build_dir}

.PHONY: clean-cache
clean-cache:
	go clean -cache -modcache -i -r

.PHONY: lint
# brew install golangci-lint
lint:
	go fmt ./...
	golangci-lint run
	go vet ./...
	@git ls-files '**/*go.mod' -z | xargs -0 -I{} bash -c 'cd $$(dirname {}) && if [ -f Makefile ] && go list ./... >/dev/null 2>&1; then make lint; fi'

.PHONY: build
build: clean
	@echo "Go version: $(GO_VERSION)"
	@go mod download
	@go build $(GO_BUILD_FLAGS) -ldflags="-X main.version=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)" -o ${build_dir}/server/mockzilla ./cmd/server

.PHONY: test
test:
	@if [ -z "$(PKG)" ]; then \
		go test -race $$(go list ./... | grep -v '/cmd/' | grep -v '/resources/') -skip=TestIntegration -count=1 -coverprofile=coverage.out && ./coverage-exclude.sh; \
	else \
  		go test -race ./... -skip=TestIntegration -count=1 -coverprofile=coverage.out && ./coverage-exclude.sh ./$(PKG)/...; \
	fi
	@git ls-files '**/*go.mod' -z | xargs -0 -I{} bash -c 'cd $$(dirname {}) && if [ -f Makefile ] && go list ./... >/dev/null 2>&1; then make test; fi'

.PHONY: fetch-specs
fetch-specs:
	rm -rf ./testdata/specs
	git clone https://github.com/mockzilla/specs.git ./testdata/specs
	find ./testdata/specs -mindepth 1 -name ".*" -exec rm -rf {} +

.PHONY: test-integration
test-integration:
	@bash -c 'set -o pipefail && SPEC="$(SPEC)" SPECS="$(or $(SPECS),$(filter-out $@,$(MAKECMDGOALS)))" go test -v -run=TestIntegration -timeout=$(or $(TEST_TIMEOUT),60m) -count=1 . 2>&1 | grep -v "^=== RUN"'
# Usage: SPECS=3.0/misc make test-integration
# Usage: SPECS="spec1.yml spec2.yml" make test-integration
# Usage: MAX_CONCURRENCY=8 make test-integration
# Usage: MAX_FAILS=5 make test-integration
# Usage: TEST_TIMEOUT=120m make test-integration
# Each service runs independently: setup → generate → build → start → test → stop
# Aborts early when MAX_FAILS is reached. Generated code kept in .sandbox/

.PHONY: test-with-check-coverage
test-with-check-coverage: test
	@coverage=$$(go tool cover -func=coverage.out | awk '/^total:/{print $$3}' | tr -d '%'); \
	echo "Code coverage $$coverage%."; \
	if [ "$$(echo "$$coverage < $(MIN_COVERAGE)" | bc -l)" -eq 1 ]; then \
	  echo "Code coverage $$coverage% is less than $(MIN_COVERAGE)%."; \
	  exit 1; \
	fi

.PHONY: gen-discover
gen-discover:
	@echo "Discovering services to generate service imports..."
	@go run ./cmd/gen/discover

.PHONY: gen-service
gen-service:
	@if [ -z "$(filter-out $@,$(MAKECMDGOALS))" ] && [ -z "$(type)" ]; then \
		echo "Error: Please provide a path to the OpenAPI spec or static directory."; \
		echo "Usage: make gen-service <path-to-spec> name=<service-name>"; \
		echo "       make gen-service <url> name=<service-name>"; \
		echo "       make gen-service name=<service-name> type=static"; \
		echo "       make gen-service <path> name=<service-name> type=static"; \
		exit 1; \
	fi
	@if [ -z "$(name)" ]; then \
		echo "Error: Service name is required."; \
		echo "Usage: make gen-service <path-to-spec> name=<service-name>"; \
		exit 1; \
	fi
	@SPEC_PATH="$(filter-out $@,$(MAKECMDGOALS))"; \
	FLAGS="-name $(name)"; \
	if [ -n "$(type)" ]; then \
		FLAGS="$$FLAGS -type $(type)"; \
	fi; \
	if [ -n "$(output)" ]; then \
		FLAGS="$$FLAGS -output $(output)"; \
	fi; \
	if [ -n "$(codegen-config)" ]; then \
		FLAGS="$$FLAGS -codegen-config $(codegen-config)"; \
	fi; \
	if [ -n "$(service-config)" ]; then \
		FLAGS="$$FLAGS -service-config $(service-config)"; \
	fi; \
	go run ./cmd/gen/service $$FLAGS "$$SPEC_PATH"
# Usage: make gen-service testdata/specs/3.0/misc/petstore.swagger.io.yml name=petstore
# Usage: make gen-service https://petstore3.swagger.io/api/v3/openapi.json name=petstore
# Usage: make gen-service name=myservice type=static
# Usage: make gen-service path/to/static name=myservice type=static
# Usage: make gen-service spec.yml name=myservice output=./services

.PHONY: gen-fakes
gen-fakes:
	@echo "Generating fake.yml from available fake functions..."
	@go run ./cmd/gen/fakes

.PHONY: gen-simplify
gen-simplify:
	@if [ -z "$(filter-out $@,$(MAKECMDGOALS))" ]; then \
		echo "Error: Please provide a path to the OpenAPI spec."; \
		echo "Usage: make gen-simplify <path-to-spec>"; \
		echo "       make gen-simplify <path-to-spec> output=<output-file>"; \
		exit 1; \
	fi
	@SPEC_PATH="$(filter-out $@,$(MAKECMDGOALS))"; \
	if [ -n "$(output)" ]; then \
		go run ./cmd/gen/simplify -output "$(output)" "$$SPEC_PATH"; \
	else \
		go run ./cmd/gen/simplify "$$SPEC_PATH"; \
	fi
# Usage: make gen-simplify openapi.yml
# Usage: make gen-simplify openapi.yml output=simplified.yml

.PHONY: generate
generate:
	@go generate ./...
	@git ls-files '**/*go.mod' -z | xargs -0 -I{} bash -c 'cd $$(dirname {}) && if [ -f Makefile ] && go list ./... >/dev/null 2>&1; then make generate; fi'

.PHONY: tidy
tidy:
	@go mod tidy
	@git ls-files '**/*go.mod' -z | xargs -0 -I{} bash -c 'cd $$(dirname {}) && if [ -f Makefile ]; then make tidy; fi'

.PHONY: check-fmt
check-fmt:
	@UNFORMATTED=$$(gofmt -l pkg cmd); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo >&2 "ERROR: The following files are not formatted:"; \
		echo "$$UNFORMATTED"; \
		exit 1; \
	fi
	@git ls-files '**/*go.mod' -z | xargs -0 -I{} bash -c 'cd $$(dirname {}) && if [ -f Makefile ] && go list ./... >/dev/null 2>&1; then make check-fmt; fi'

.PHONY: server
server:
	@echo "Building and starting development server with built-in hot-reload..."
	@mkdir -p ${build_dir}/server
	@go build -ldflags="-X main.version=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)" -o ${build_dir}/server/server ./cmd/server
	@${build_dir}/server/server

.PHONY: docker-build
docker-build: build
	@docker build \
		--no-cache . \
		--tag $(IMAGE_NAME):latest \
		--tag $(IMAGE_NAME):$(VERSION)

.PHONY: docker-build-multiarch
docker-build-multiarch:
	@docker buildx build \
		--platform linux/arm64,linux/amd64 \
		--no-cache . \
		--tag $(IMAGE_NAME):latest \
		--tag $(IMAGE_NAME):$(VERSION) \
		--push

.PHONY: docker-push
docker-push:
	@docker push $(IMAGE_NAME):$(VERSION)
	@docker push $(IMAGE_NAME):latest

.PHONY: docker-run
docker-run:
	@docker run -it --rm \
		--platform=linux/arm64 \
		-p 2200:2200 \
		-e LOG_FORMAT=text \
		-v ~/cxs-data:/app/resources/data/services \
		$(IMAGE_NAME) api

@PHONY: docker-shell
docker-shell:
	@docker exec -it $$(docker ps -q -f ancestor=$(IMAGE_NAME)) bash

@PHONY: docs-install
docs-install:
	@brew install mkdocs-material

@PHONY: docs-prepare
docs-prepare:
	@go run ./cmd/fake_list/main.go

@PHONY: docs-serve
docs-serve: docs-prepare
	mkdocs serve

@PHONY: docs-deploy
docs-deploy: docs-prepare
	mkdocs gh-deploy --force

# Suppress "is up to date" messages for positional arguments (spec files, URLs)
# These get passed to MAKECMDGOALS and treated as targets.
# testdata paths are handled explicitly since they match real files.
# The catch-all %: rule handles URLs and other unknown targets.
.PHONY: $(filter testdata/%, $(MAKECMDGOALS))
$(filter testdata/%, $(MAKECMDGOALS)):
	@:
%:
	@:
