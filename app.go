package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Config struct {
	MaxLines    int
	Files       []string
	RefreshRate int
	Include     string
	Exclude     string
	Timezone    string
}

type App struct {
	config   *Config
	model    *Model
	parser   *LogParser
	program  *tea.Program
}

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

func (l LogLevel) Color() lipgloss.Color {
	switch l {
	case DEBUG:
		return lipgloss.Color("8")   // Gray
	case INFO:
		return lipgloss.Color("12")  // Light Blue
	case WARN:
		return lipgloss.Color("11")  // Yellow
	case ERROR:
		return lipgloss.Color("9")   // Red
	default:
		return lipgloss.Color("15")  // White
	}
}

type LogEntry struct {
	Timestamp string
	Level     LogLevel
	Message   string
	Source    string
	Raw       string
	Metadata  map[string]interface{}
}

type CircularBuffer struct {
	entries []LogEntry
	head    int
	tail    int
	size    int
	maxSize int
}

func NewCircularBuffer(maxSize int) *CircularBuffer {
	return &CircularBuffer{
		entries: make([]LogEntry, maxSize),
		maxSize: maxSize,
	}
}

func (cb *CircularBuffer) Add(entry LogEntry) {
	cb.entries[cb.head] = entry
	cb.head = (cb.head + 1) % cb.maxSize
	
	if cb.size < cb.maxSize {
		cb.size++
	} else {
		cb.tail = (cb.tail + 1) % cb.maxSize
	}
}

func (cb *CircularBuffer) GetAll() []LogEntry {
	if cb.size == 0 {
		return []LogEntry{}
	}
	
	result := make([]LogEntry, cb.size)
	for i := 0; i < cb.size; i++ {
		idx := (cb.tail + i) % cb.maxSize
		result[i] = cb.entries[idx]
	}
	return result
}

func NewApp(config *Config) *App {
	app := &App{
		config: config,
		parser: NewLogParser(config.Timezone),
	}
	
	model := NewModel(config)
	app.model = model
	
	return app
}

func (a *App) Run() error {
	// Create the Bubbletea program
	a.program = tea.NewProgram(a.model, tea.WithAltScreen())
	
	// Start input processing in background after program is created
	go a.processInput()
	
	// Run the program
	if _, err := a.program.Run(); err != nil {
		return fmt.Errorf("failed to run program: %w", err)
	}
	
	return nil
}

func (a *App) processInput() {
	// Small delay to ensure the program is fully initialized
	time.Sleep(100 * time.Millisecond)
	
	// Check if we have piped input
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		// We have piped input
		a.readFromStdin()
		return
	}
	
	// Process files if specified
	if len(a.config.Files) > 0 {
		for _, file := range a.config.Files {
			go a.readFromFile(file)
		}
		return
	}
	
	// No input specified, wait for user interaction
}

func (a *App) readFromStdin() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		entry := a.parseLogLine(line)
		a.sendLogEntry(entry)
	}
}

func (a *App) readFromFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()
	
	// For now, read the entire file. Later we can implement tail-like functionality
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry := a.parser.ParseLogLine(line, filename)
		a.sendLogEntry(entry)
	}
}

func (a *App) parseLogLine(line string) LogEntry {
	return a.parser.ParseLogLine(line, "")
}

func (a *App) sendLogEntry(entry LogEntry) {
	if a.program != nil {
		// Send the log entry as a message to the UI
		a.program.Send(LogEntryMsg(entry))
	}
}