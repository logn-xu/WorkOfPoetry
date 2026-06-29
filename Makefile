APP := workofpoetry
PKG := ./cmd/workofpoetry
DIST_DIR := dist

.PHONY: help fmt vet test build build-windows clean tidy run

help:
	@echo "Targets:"
	@echo "  fmt           Format Go source files"
	@echo "  vet           Run go vet"
	@echo "  test          Run Go tests"
	@echo "  build         Build for current platform"
	@echo "  build-windows Build Windows amd64 executable"
	@echo "  tidy          Tidy module dependencies"
	@echo "  clean         Remove build outputs"
	@echo "  run           Run locally; pass SSH args with: make run ARGS='-- user@host'"

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f | sort)

vet:
	go vet ./...

test:
	go test ./...

build: fmt
	mkdir -p $(DIST_DIR)
	go build -o $(DIST_DIR)/$(APP) $(PKG)

build-windows: fmt
	mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/$(APP).exe $(PKG)

tidy:
	go mod tidy

clean:
	rm -rf $(DIST_DIR) $(APP) $(APP).exe

run:
	go run $(PKG) $(ARGS)
