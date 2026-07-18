.PHONY: build release clean benchmark

build:
	go build -o grubber .

release:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o grubber-macos-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o grubber-macos-amd64 .
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o grubber-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o grubber-linux-arm64 .

clean:
	rm -f grubber grubber-macos-arm64 grubber-macos-amd64 grubber-linux-amd64 grubber-linux-arm64

benchmark:
	go build -o grubber . && hyperfine --warmup 3 './grubber extract $(GRUBBER_NOTES) -o /dev/null'
