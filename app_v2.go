package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbletea"
)

// AppV2 is the optimized version with virtual scrolling
type AppV2 struct {
	config  *Config
	model   *VirtualModel
	parser  *LogParser
	reader  *StreamReader
	program *tea.Program
}

func NewAppV2(config *Config) *AppV2 {
	parser := NewLogParser(config.Timezone)
	model := NewVirtualModel(config)
	
	app := &AppV2{
		config: config,
		model:  model,
		parser: parser,
	}
	
	return app
}

func (a *AppV2) Run() error {
	// Create the Bubbletea program
	a.program = tea.NewProgram(a.model, tea.WithAltScreen())
	
	// Create stream reader with program reference
	a.reader = NewStreamReader(a.config, a.parser, a.program)
	a.model.SetReader(a.reader)
	
	// Start processing input in background with small delay
	// to ensure program is fully initialized
	go func() {
		time.Sleep(10 * time.Millisecond)
		a.reader.ProcessInput()
	}()
	
	// Run the program
	if _, err := a.program.Run(); err != nil {
		return fmt.Errorf("failed to run program: %w", err)
	}
	
	return nil
}

// Fallback to use the optimized version by default
func init() {
	// Check for environment variable to use new version
	if os.Getenv("PANAM_V2") != "false" {
		// Use V2 by default
		useV2 = true
	}
}

var useV2 = true