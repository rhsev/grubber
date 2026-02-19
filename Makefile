.PHONY: build clean test benchmark

# Default: build Crystal binary
build: grubber_crystal

# Build optimized Crystal binary
grubber_crystal: grubber.cr
	crystal build grubber.cr -o grubber_crystal --release

# Development build (faster compile, slower runtime)
dev: grubber.cr
	crystal build grubber.cr -o grubber_crystal

# Clean build artifacts
clean:
	rm -f grubber_crystal

# Run both versions for comparison
test:
	@echo "=== Ruby ===" && ./grubber extract . --blocks-only 2>/dev/null | head -20
	@echo "\n=== Crystal ===" && ./grubber_crystal extract . --blocks-only 2>/dev/null | head -20

# Benchmark both versions
benchmark:
	@echo "Ruby:" && time ./grubber extract $(GRUBBER_NOTES) > /dev/null 2>&1
	@echo "Crystal:" && time ./grubber_crystal extract $(GRUBBER_NOTES) > /dev/null 2>&1
