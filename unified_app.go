package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// UnifiedApp is the single fast version with all features
type UnifiedApp struct {
	config  *Config
	model   *UnifiedModel
	program *tea.Program
}

func NewUnifiedApp(config *Config) *UnifiedApp {
	model := NewUnifiedModel(config)
	return &UnifiedApp{
		config: config,
		model:  model,
	}
}

func (a *UnifiedApp) Run() error {
	// Create the Bubbletea program
	a.program = tea.NewProgram(a.model, tea.WithAltScreen())
	
	// Start processing input in background
	go a.processInput()
	
	// Run the program
	if _, err := a.program.Run(); err != nil {
		return fmt.Errorf("failed to run program: %w", err)
	}
	
	return nil
}

func (a *UnifiedApp) processInput() {
	// Small delay to ensure program is initialized
	time.Sleep(10 * time.Millisecond)
	
	// Check if we have piped input
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		// Piped input - use streaming mode
		a.streamFromStdin()
		return
	}
	
	// Process files if specified
	if len(a.config.Files) > 0 {
		for _, file := range a.config.Files {
			a.indexFile(file)
		}
	}
}

func (a *UnifiedApp) indexFile(filename string) {
	// Check if file exists
	if _, err := os.Stat(filename); err != nil {
		return
	}
	
	// Create fast indexer
	indexer, err := NewFastIndexer(filename, a.model.parser)
	if err != nil {
		return
	}
	
	// Update model state
	a.model.indexing = true
	a.model.loadingFile = filename
	
	// Start indexing
	start := time.Now()
	if err := indexer.IndexFileUltraFast(); err != nil {
		indexer.Close()
		return
	}
	
	// Update model with indexer
	a.model.indexTime = time.Since(start)
	a.model.SetIndexer(indexer, filename)
}

func (a *UnifiedApp) streamFromStdin() {
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	
	batch := make([]LogEntry, 0, 100)
	lastSend := time.Now()
	
	for scanner.Scan() {
		line := scanner.Text()
		entry := a.model.parser.ParseLogLine(line, "stdin")
		batch = append(batch, entry)
		
		// Send batch
		if len(batch) >= 100 || time.Since(lastSend) > 20*time.Millisecond {
			a.sendBatch(batch)
			batch = batch[:0]
			lastSend = time.Now()
		}
	}
	
	// Send remaining
	if len(batch) > 0 {
		a.sendBatch(batch)
	}
}

func (a *UnifiedApp) sendBatch(entries []LogEntry) {
	if a.program != nil && len(entries) > 0 {
		a.program.Send(LogBatchMsg(entries))
	}
}