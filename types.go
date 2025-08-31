package main

import (
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
		return lipgloss.Color("8") // Gray
	case INFO:
		return lipgloss.Color("12") // Light Blue
	case WARN:
		return lipgloss.Color("11") // Yellow
	case ERROR:
		return lipgloss.Color("9") // Red
	default:
		return lipgloss.Color("15") // White
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

// Panel focus types
type PanelFocus int

const (
	LeftPanel PanelFocus = iota
	RightPanel
)

// View modes
type ViewMode int

const (
	LogStreamView ViewMode = iota
	DetailView
)

// Input fields
type InputField int

const (
	includeInput InputField = iota
	excludeInput
)

// Messages for TUI
type LogEntryMsg LogEntry
type LogBatchMsg []LogEntry