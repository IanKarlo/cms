APP := cms
GO := go

.PHONY: all build test check clean

all: build

build:
	$(GO) build -o $(APP) ./cmd/cms

test:
	$(GO) test ./...

check: test build

clean:
	rm -f $(APP)
