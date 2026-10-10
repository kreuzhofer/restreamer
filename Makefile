.PHONY: build test check check-web cross-build brb-artwork check-brb-artwork check-arcade-artwork check-neon-artwork
brb-artwork:
	node artwork/brb/render.mjs
check-brb-artwork:
	node --test artwork/brb/animation.test.mjs
	node artwork/brb/render.mjs --check
check-arcade-artwork:
	node artwork/arcade-after-hours/verify.mjs
check-neon-artwork:
	node artwork/neon-night/verify.mjs
build:
	CGO_ENABLED=0 go build -trimpath -o bin/restreamer ./cmd/restreamer
test:
	go test -race -timeout 20m ./...
check: test check-web check-arcade-artwork check-neon-artwork
	go vet ./...
check-web:
	node --test tests/*.test.mjs
cross-build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/restreamer-linux-amd64 ./cmd/restreamer
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/restreamer-linux-arm64 ./cmd/restreamer
