# Panam - Terminal Log Viewer

Panam is a Terminal User Interface (TUI) application designed to view and filter log files in real-time. It provides an interactive interface for developers and system administrators to navigate through logs with powerful filtering capabilities.

## Features

### Ultra-Fast Performance
- **Sub-1 second loading** for million-line files
- **Virtual scrolling** with lazy parsing
- **Minimal memory usage** - only loads visible content

### Enhanced Interface
- **Two-panel layout**: 
  - Left panel: Search filters and controls with visual indicators
  - Right panel: 3-column log display (TIME | LEVEL | MESSAGE)
- **Real-time updates**: Live log streaming with instant UI refresh
- **Detail view**: Press Enter to see full log entry with metadata

### Powerful Filtering
- **Include/exclude patterns**: Comma-separated, with regex support
- **Log level filtering**: Toggle ERROR, WARN, INFO, DEBUG levels
- **Pattern highlighting**: Matches highlighted in search results
- **Global shortcuts**: `/` for include, `\` for exclude filters

### Navigation & Controls
- **Vim-style navigation**: j/k, Ctrl+d/u, gg/G
- **Smart scrolling**: Half-page scrolling, jump to matches
- **Edit mode**: `i` to edit, Esc to exit, Space to toggle
- **Tab navigation**: Switch between panels seamlessly

### Format Support
- **OTLP**: Full OpenTelemetry Log Protocol support
- **Rails logs**: SQL timing, ANSI color handling
- **Structured logs**: JSON, Apache/Nginx formats
- **Plain text**: Auto-detection of levels and timestamps

## Installation

```bash
git clone <repository>
cd panam
go build -o panam
```

## Usage

### Basic Usage

```bash
# View a log file
./panam -e /var/log/app.log

# Pipe logs from another command
tail -f /var/log/app.log | ./panam

# Process multiple files
./panam -e file1.log -e file2.log

# Set memory limit
./panam -m 5000 -e /var/log/app.log
```

### Command-line Options

- `--max_line/-m`: Maximum lines to keep in memory (default: 10000)
- `--files/-e`: List of files to process (can be used multiple times)
- `--refresh_rate/-r`: Refresh rate in seconds (default: 1)
- `--include/-i`: Default include filter patterns (comma-separated)
- `--exclude/-x`: Default exclude filter patterns (comma-separated)
- `--timezone`: Display timezone for timestamps (default: UTC)

### Keyboard Controls

#### Navigation
- `Tab`: Switch between left and right panels
- `↑/k`: Move selection up
- `↓/j`: Move selection down
- `Home`: Go to first entry
- `End`: Go to last entry

#### Filtering (Quick Access)
- `i`: Quick access to include filter input
- `e`: Quick access to exclude filter input
- `/`: Focus on include filter input (alternative)
- `\`: Focus on exclude filter input (alternative)
- `c`: Clear all filters
- `1-4`: Toggle log levels (1=ERROR, 2=WARN, 3=INFO, 4=DEBUG)
- `Enter` (in filter input): Apply filters and return to log view
- `ESC` (in filter input): Cancel input and return to log view

#### Actions
- `Enter`: Show detailed view of selected log entry in right panel
- `ESC/q`: Return to log stream from detail view
- `q/Ctrl+C`: Quit application

## Log Format Support

### OTLP (OpenTelemetry Log Protocol)
Supports full OTLP JSON format with:
- Timestamp conversion from Unix nano
- Severity level mapping
- Attribute and metadata extraction
- Resource information

### Rails Logs
Automatically detects and parses Rails application logs:
- SQL query timing extraction
- ANSI color code handling
- Automatic DEBUG level assignment for database operations

### Structured Logs
- Apache/Nginx common log format
- JSON structured logs
- Custom timestamp extraction

### Plain Text
- Automatic log level detection (ERROR, WARN, INFO, DEBUG)
- Timestamp extraction from common formats
- Fallback parsing for any text format

## Architecture

### Ultra-Fast Performance Design

Panam achieves **sub-1 second loading** for million-line log files through an innovative architecture inspired by [lnav](https://github.com/tstack/lnav). The key innovation is **virtual scrolling with lazy parsing** - we only parse what's visible on screen.

#### Performance Benchmarks
- **1.2M lines (191MB file)**: Indexed in 95ms
- **Line retrieval**: 293 microseconds for 100 lines
- **Total startup**: <1 second (vs 20+ seconds with traditional approaches)
- **Memory usage**: Minimal (only stores byte offsets + visible lines)

### Core Architecture Components

1. **Fast Indexer** (`fast_indexer.go`)
   - Scans files in a single pass, storing only byte offsets
   - No parsing during indexing phase
   - Uses 256KB buffer for efficient I/O
   - Pre-allocates arrays based on file size estimation

2. **Virtual Scrolling** (`unified_model.go`)
   - Only parses and renders visible lines (typically 40-50)
   - Lazy parsing on scroll events
   - Smooth scrolling through millions of lines

3. **Smart Caching**
   - 5000 entry LRU cache for parsed lines
   - Batch retrieval for consecutive line ranges
   - Cache-aware scrolling algorithms

4. **Unified App** (`unified_app.go`)
   - Single implementation, no confusing modes
   - Fast by default for all file sizes
   - Seamless handling of both files and streams

### How It Works

```
1. File Loading
   ├── Quick scan to build line index (offsets only)
   ├── No parsing, just newline detection
   └── Completes in <100ms for millions of lines

2. Display
   ├── Parse only visible viewport (40-50 lines)
   ├── Retrieve lines using stored offsets
   └── Update display in microseconds

3. Scrolling
   ├── Load new viewport on demand
   ├── Check cache first
   └── Parse only if not cached
```

### Key Components

- `main.go`: Application entry point and CLI setup
- `unified_app.go`: Main application orchestrator
- `unified_model.go`: TUI model with full feature set
- `fast_indexer.go`: Ultra-fast file indexing engine
- `parser.go`: Multi-format log parsing engine
- `*_test.go`: Comprehensive test suite

## Testing

Run the test suite:

```bash
# Unit tests
go test -v

# Integration tests
go test -run Integration -v

# Benchmarks
go test -bench=. -v
```

## Performance

### Benchmarks

- **File indexing**: 95ms for 1.2M lines (191MB file)
- **Line retrieval**: 293μs for 100 lines from cache
- **Log parsing**: ~9.6μs per log line when needed
- **Memory usage**: O(n) for line offsets, O(1) for visible content
- **Startup time**: <1 second for any file size

### Optimizations

- **Zero-copy indexing**: Only stores byte offsets during initial scan
- **Lazy evaluation**: Parses only when content is viewed
- **Smart caching**: LRU cache reduces repeated parsing
- **Batch I/O**: Reads consecutive lines in single operation
- **Virtual scrolling**: Constant memory regardless of file size

## Development

The codebase follows Go best practices:
- Comprehensive test coverage
- Benchmarks for performance-critical components
- Clean separation of concerns
- Extensive documentation

### Contributing

1. Keep code simple and testable
2. Always add tests for new features
3. Run the full test suite before committing
4. Follow existing code style and patterns

## Quick Start

```bash
make build          # Build panam
make test           # Run tests  
make demo           # Show help and validate build
make run            # Get instructions for running with sample logs
make help           # See all options
```

**Note**: Panam is a Terminal User Interface (TUI) application that requires an interactive terminal. It cannot be run directly in non-interactive environments, but you can:

- Build and test the application using `make demo` and `make test`
- Run it in your terminal with: `./panam -e tmp/small_test.log`
- Use it with piped input: `head -100 tmp/test.log | ./panam`

## License

[Add your license information here]