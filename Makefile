.PHONY: build test cover lint fmt vet tidy check install clean

VERSION ?= dev
LDFLAGS := -X main.Version=$(VERSION)

build:
	go build -ldflags '$(LDFLAGS)' -o stringer ./cmd/stringer

test:
	go test -race -count=1 ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

fmt:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Files not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet ./...

tidy:
	go mod tidy
	@git diff --exit-code go.mod || (echo "go.mod not tidy" && exit 1)

check: fmt vet lint test

install:
	go install -ldflags '$(LDFLAGS)' ./cmd/stringer

clean:
	rm -f stringer coverage.out
