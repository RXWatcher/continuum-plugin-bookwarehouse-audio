BINARY := silo-plugin-bookwarehouse-audio
GO ?= go

.PHONY: build test clean
build:
	$(GO) build -o $(BINARY) ./cmd/silo-plugin-bookwarehouse-audio
test:
	$(GO) test ./...
clean:
	rm -f $(BINARY)
