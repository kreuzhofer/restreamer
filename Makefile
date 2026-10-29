.PHONY: build test check check-web cross-build brb-artwork check-brb-artwork check-arcade-artwork check-neon-artwork check-workshop-artwork pacman-artwork check-pacman-artwork
brb-artwork:
	node artwork/brb/render.mjs
check-brb-artwork:
	node --test artwork/brb/animation.test.mjs
	node artwork/brb/render.mjs --check
check-arcade-artwork:
	node artwork/arcade-after-hours/verify.mjs
check-neon-artwork:
	node artwork/neon-night/verify.mjs
check-workshop-artwork:
	node artwork/maker-workshop/verify.mjs
pacman-artwork:
	node artwork/pacman-maze/render.mjs
check-pacman-artwork:
	node --test artwork/pacman-maze/motion.test.mjs
	node artwork/pacman-maze/render.mjs --check
build:
	CGO_ENABLED=0 go build -trimpath -o bin/restreamer ./cmd/restreamer
test:
	go test -race -timeout 20m ./...
check: test check-web check-streamdeck check-arcade-artwork check-neon-artwork check-workshop-artwork check-pacman-artwork
	go vet ./...
check-web:
	node --test tests/*.test.mjs
.PHONY: check-streamdeck
check-streamdeck:
	npm --prefix integrations/streamdeck ci --ignore-scripts
	npm --prefix integrations/streamdeck test
	npm --prefix integrations/streamdeck run pack
cross-build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/restreamer-linux-amd64 ./cmd/restreamer
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/restreamer-linux-arm64 ./cmd/restreamer
