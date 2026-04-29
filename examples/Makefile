.PHONY: generate tidy test lint lint-ci tidy-ci check-fmt

generate:
	go generate ./...

tidy:
	go mod tidy

test:
	go test -cover ./...

lint:
	golangci-lint run ./...

lint-ci:
	golangci-lint run ./... --timeout=5m

tidy-ci:
	tidied -verbose

check-fmt:
	@UNFORMATTED=$$(gofmt -l .); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo >&2 "ERROR: The following files are not formatted:"; \
		echo "$$UNFORMATTED"; \
		exit 1; \
	fi
