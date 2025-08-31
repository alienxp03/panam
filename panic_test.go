package main

import (
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestPanic_SmallTerminalDimensions(t *testing.T) {
	// Test various small terminal dimensions to ensure no panics occur
	testCases := []struct {
		width  int
		height int
		name   string
	}{
		{10, 5, "very small"},
		{20, 10, "small"},
		{30, 15, "narrow"},
		{40, 20, "minimal"},
		{50, 25, "compact"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				MaxLines:    100,
				Files:       []string{},
				RefreshRate: 1,
				Include:     "",
				Exclude:     "",
				Timezone:    "UTC",
			}
			
			model := NewUnifiedModel(config)
			
			// Add some test entries
			entries := []LogEntry{
				{Message: "Test INFO message", Level: INFO, Timestamp: "2023-12-23 15:30:45", Source: "test.log"},
				{Message: "Test ERROR message", Level: ERROR, Timestamp: "2023-12-23 15:30:46", Source: "test.log"},
				{Message: "Test WARN message", Level: WARN, Timestamp: "2023-12-23 15:30:47", Source: "test.log"},
			}
			
			for _, entry := range entries {
				model.AddLogEntry(entry)
			}
			
			// Test window size message processing
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Panic occurred with dimensions %dx%d: %v", tc.width, tc.height, r)
				}
			}()
			
			// Simulate window size change
			model.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			
			// Test rendering with small dimensions
			view := model.View()
			
			if view == "" {
				t.Errorf("View rendering returned empty string for dimensions %dx%d", tc.width, tc.height)
			}
			
			t.Logf("Successfully rendered view for %dx%d dimensions", tc.width, tc.height)
		})
	}
}

func TestPanic_NegativeWidthCalculations(t *testing.T) {
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	model := NewUnifiedModel(config)
	
	// Test direct method calls with problematic widths
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Panic occurred in width calculations: %v", r)
		}
	}()
	
	// Initialize required fields
	model.levelStyles = map[LogLevel]lipgloss.Style{
		ERROR: lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		WARN:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		INFO:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		DEBUG: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
	
	// Test with problematic widths
	problematicWidths := []int{-10, -1, 0, 1, 2, 3, 4, 5}
	
	for _, width := range problematicWidths {
		// Test renderLogHeader
		header := model.renderLogHeader(width)
		if header == "" && width > 0 {
			t.Logf("Warning: renderLogHeader returned empty string for width %d", width)
		}
		
		// Test formatLogEntryColumns
		entry := LogEntry{
			Message:   "Test message",
			Level:     INFO,
			Timestamp: "2023-12-23 15:30:45",
			Source:    "test.log",
		}
		
		formatted := model.formatLogEntryColumns(entry, width)
		if formatted == "" && width > 0 {
			t.Logf("Warning: formatLogEntryColumns returned empty string for width %d", width)
		}
		
		t.Logf("Successfully handled width %d", width)
	}
}

func TestPanic_RenderWithNoEntries(t *testing.T) {
	// Test rendering with no log entries to ensure no divide-by-zero or similar issues
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewUnifiedModel(config)
	
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Panic occurred with no entries: %v", r)
		}
	}()
	
	// Set various window sizes and try rendering
	sizes := []tea.WindowSizeMsg{
		{Width: 80, Height: 24},
		{Width: 40, Height: 12},
		{Width: 20, Height: 8},
		{Width: 10, Height: 5},
	}
	
	for _, size := range sizes {
		model.Update(size)
		view := model.View()
		
		if view == "" {
			t.Errorf("View rendering returned empty string for size %dx%d with no entries", size.Width, size.Height)
		}
		
		t.Logf("Successfully rendered empty view for %dx%d", size.Width, size.Height)
	}
}