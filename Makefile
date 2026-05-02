.PHONY: build release clean benchmark

build:
	go build -o grubber .

release:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o grubber-macos-arm64 .

clean:
	rm -f grubber grubber-macos-arm64

benchmark:
	go build -o grubber . && hyperfine --warmup 3 './grubber extract $(GRUBBER_NOTES) -o /dev/null'
