.PHONY: build

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o bin/tianji-monitor-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o bin/tianji-monitor-linux-arm64 .
	cd bin && shasum -a 256 tianji-monitor-linux-* > SHA256SUMS
