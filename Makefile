.PHONY: dev fmt lint test audit

dev:
	go run ./cmd/devserver

fmt:
	gofmt -w cmd internal tests

lint:
	test -z "$$(gofmt -l cmd internal tests)"
	go vet ./...

test:
	go test ./...

audit:
	bash scripts/check-public-content.sh
	$(MAKE) lint
	$(MAKE) test
