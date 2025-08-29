# Panam - Terminal Log Viewer

Panam is a Terminal User Interface (TUI) application designed to view and filter log files in real-time. It provides an interactive interface for developers and system administrators to navigate through logs with powerful filtering capabilities.

## Features

- **Enhanced two-panel interface**: 
  - Left panel: Beautiful filters and controls with emoji icons and statistics
  - Right panel: Columnar log display with TIME │ LEVEL │ SOURCE │ MESSAGE format
- **Real-time log processing**: Support for piped input and file reading
- **Multi-format support**: OTLP (OpenTelemetry Log Protocol), Rails logs, structured logs, and plain text
- **Advanced filtering**: Include/exclude patterns with regex support
- **Log level filtering**: Visual checkboxes with color-coded levels (✅/❌)
- **Memory efficient**: Configurable circular buffer to manage memory usage
- **Quick keyboard access**: 
  - `i`/`e` hotkeys for instant filter access
  - Vim-style navigation (j/k) plus arrow keys
- **Enhanced detail view**: In-panel detailed view showing full log entry with syntax highlighting
- **Visual improvements**:
  - Color-coded log levels with consistent styling
  - Duration display for SQL queries and timed operations
  - Progress indicators and filter efficiency statistics
  - Contextual help and status information

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

The application is built with:
- **Bubbletea**: TUI framework for the terminal interface
- **Cobra**: Command-line interface and argument parsing
- **Circular Buffer**: Memory-efficient log storage
- **Multi-format Parser**: Extensible parsing engine

### Key Components

- `main.go`: Application entry point and CLI setup
- `app.go`: Core application logic and configuration
- `model.go`: TUI model with two-panel layout and state management
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

- **Log parsing**: ~9.6μs per log line
- **Circular buffer**: ~5ns per add operation
- **Memory usage**: Configurable with `--max_line` parameter
- **Real-time processing**: Optimized for continuous log streams

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