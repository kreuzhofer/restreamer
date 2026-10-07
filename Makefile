.PHONY: build test check cross-build
build:
	CGO_ENABLED=0 go build -trimpath -o bin/restreamer ./cmd/restreamer
test:
	go test -race ./...
check: test
	go vet ./...
cross-build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/restreamer-linux-amd64 ./cmd/restreamer
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/restreamer-linux-arm64 ./cmd/restreamer
