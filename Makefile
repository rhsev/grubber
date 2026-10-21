BIN    := grubber
BINDIR ?= $(HOME)/bin
PREFIX ?= /usr/local

# Stamp the version from the tag rather than trusting the literal in main.go,
# which had gone two releases without being bumped. A dirty or untagged tree
# says so in `grubber -v`.
VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//')
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install uninstall test link unlink release clean benchmark

# Two commands: grubber (reads the blocks) and commenter (reads and replaces
# an entry's prose, package entry).
build:
	go build -ldflags="$(LDFLAGS)" -o grubber .
	go build -ldflags="$(LDFLAGS)" -o commenter ./cmd/commenter

release:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-macos-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-macos-amd64 .
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o grubber-linux-arm64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o commenter-macos-arm64 ./cmd/commenter
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o commenter-macos-amd64 ./cmd/commenter
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o commenter-linux-amd64 ./cmd/commenter
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o commenter-linux-arm64 ./cmd/commenter

# Link once, then never again: rebuilding is deploying. A stale copy fails silently,
# a dangling link fails at the next call. Convention: ../BUILD.md.
link: build
	@install -d $(BINDIR)
	@ln -sfn $(CURDIR)/$(BIN) $(BINDIR)/$(BIN)
	@ln -sfn $(CURDIR)/commenter $(BINDIR)/commenter
	@echo "linked $(BINDIR)/$(BIN) and $(BINDIR)/commenter -> $(CURDIR)"

unlink:
	rm -f $(BINDIR)/$(BIN) $(BINDIR)/commenter

# Published repo: `install` copies into $(PREFIX)/bin for anyone who clones
# this. A clone is not a stable place to point a symlink at — the symlink
# form is `make link`, for the machine this is developed on. See ../BUILD.md.
install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)
	install -m 755 commenter $(PREFIX)/bin/commenter

uninstall:
	rm -f $(PREFIX)/bin/$(BIN) $(PREFIX)/bin/commenter

test:
	go test ./...

clean:
	rm -f grubber grubber-macos-arm64 grubber-macos-amd64 grubber-linux-amd64 grubber-linux-arm64
	rm -f commenter commenter-macos-arm64 commenter-macos-amd64 commenter-linux-amd64 commenter-linux-arm64

benchmark:
	go build -o grubber . && hyperfine --warmup 3 './grubber extract $(GRUBBER_NOTES) -o /dev/null'
