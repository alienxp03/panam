.PHONY: build run test clean install help

# Default target
all: build

# Build the application
build:
	go build -o panam

# Run the application with example file (requires interactive terminal)
run: build
	@echo "Note: This requires an interactive terminal (TTY)"
	@echo "Run this command in your terminal: ./panam -e tmp/small_test.log"
	@echo "Or try: head -50 tmp/test.log | ./panam"
	./panam -e tmp/small_test.log

# Run with piped input (example)
pipe: build
	@echo "Note: This requires an interactive terminal (TTY)"
	@echo "Run this command in your terminal: head -50 tmp/test.log | ./panam"

# Run all tests
test:
	go test -v

# Run benchmarks
bench:
	go test -bench=. -v

# Run integration tests only
integration:
	go test -run Integration -v

# Demo - show help and validate build (works in non-TTY environments)
demo: build
	@echo "=== Panam Log Viewer Demo ==="
	@echo "Showing help output:"
	@echo ""
	./panam --help
	@echo ""
	@echo "=== Build successful! ==="
	@echo "To use interactively, run in your terminal:"
	@echo "  ./panam -e tmp/small_test.log"
	@echo "  head -100 tmp/test.log | ./panam"

# Install dependencies
deps:
	go mod tidy

# Clean build artifacts
clean:
	rm -f panam

# Install to system PATH
install: build
	cp panam /usr/local/bin/

# Show help
help:
	@echo "Available targets:"
	@echo "  build       - Build the panam binary"
	@echo "  demo        - Show help and validate build (works anywhere)"
	@echo "  run         - Instructions for running with example log file"
	@echo "  pipe        - Instructions for running with piped input"
	@echo "  test        - Run all tests"
	@echo "  bench       - Run benchmarks"
	@echo "  integration - Run integration tests only"
	@echo "  deps        - Install/update dependencies"
	@echo "  clean       - Remove build artifacts"
	@echo "  install     - Install to /usr/local/bin"
	@echo "  help        - Show this help message"
