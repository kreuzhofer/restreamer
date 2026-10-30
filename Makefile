.PHONY: check-continuous-benchmark build test check check-web cross-build brb-artwork check-brb-artwork check-arcade-artwork check-neon-artwork check-workshop-artwork pacman-artwork check-pacman-artwork
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
check-continuous-benchmark:
	go build -race -o bin/continuous-bench-sinks ./experiments/continuous-output/sinks
	go build -o bin/continuous-bench-relay ./cmd/restreamer
	python3 -m unittest discover -s experiments/continuous-output -v
build:
	CGO_ENABLED=0 go build -trimpath -o bin/restreamer ./cmd/restreamer
test:
	go test -race -timeout 20m ./...
check: test check-continuous-benchmark check-web check-arcade-artwork check-neon-artwork check-workshop-artwork check-pacman-artwork
	go vet ./...
check-web:
	node --test tests/*.test.mjs
cross-build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/restreamer-linux-amd64 ./cmd/restreamer
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/restreamer-linux-arm64 ./cmd/restreamer
