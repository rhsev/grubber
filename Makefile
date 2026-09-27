BIN    := grubber
BINDIR ?= $(HOME)/bin
PREFIX ?= /usr/local

# Stamp the version from the tag rather than trusting the literal in main.go,
# which had gone two releases without being bumped. A dirty or untagged tree
# says so in `grubber -v`.
VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//')
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install uninstall test link unlink release clean benchmark

build:
	go build -ldflags="$(LDFLAGS)" -o grubber .

release:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-macos-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-macos-amd64 .
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-linux-arm64 .

# Link once, then never again: rebuilding is deploying. A stale copy fails silently,
# a dangling link fails at the next call. Convention: ../BUILD.md.
link: build
	@install -d $(BINDIR)
	@ln -sfn $(CURDIR)/$(BIN) $(BINDIR)/$(BIN)
	@echo "linked $(BINDIR)/$(BIN) -> $(CURDIR)/$(BIN)"

unlink:
	rm -f $(BINDIR)/$(BIN)

# Published repo: `install` copies into $(PREFIX)/bin for anyone who clones
# this. A clone is not a stable place to point a symlink at — the symlink
# form is `make link`, for the machine this is developed on. See ../BUILD.md.
install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

test:
	go test ./...

clean:
	rm -f grubber grubber-macos-arm64 grubber-macos-amd64 grubber-linux-amd64 grubber-linux-arm64

benchmark:
	go build -o grubber . && hyperfine --warmup 3 './grubber extract $(GRUBBER_NOTES) -o /dev/null'
