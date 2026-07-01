BIN := chartbump
GO  := go

.PHONY: build install run clean tidy lint

build:
	$(GO) build -o $(BIN) .

install:
	$(GO) install .

run:
	$(GO) run . $(ARGS)

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN)

lint:
	golangci-lint run ./...
