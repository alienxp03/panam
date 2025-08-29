package main

import (
	"os"
	"testing"
)

func TestDebug_ProcessLogFile(t *testing.T) {
	// Test if processing the log file directly causes any panics
	config := &Config{
		MaxLines:    100,
		Files:       []string{"tmp/small_test.log"},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}

	app := NewApp(config)

	// Try to process the file without starting the TUI
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Panic occurred while processing log file: %v", r)
		}
	}()

	// This should not panic
	app.readFromFile("tmp/small_test.log")

	entries := app.model.buffer.GetAll()
	if len(entries) == 0 {
		t.Error("No entries were processed from the log file")
	}

	t.Logf("Successfully processed %d entries", len(entries))
}

func TestDebug_LargeLogFile(t *testing.T) {
	// Test with a portion of the larger log file to see if size causes issues

	// Check if the development.log file exists
	if _, err := os.Stat("tmp/development.log"); os.IsNotExist(err) {
		t.Skip("development.log not available for testing")
	}

	config := &Config{
		MaxLines:    1000,
		Files:       []string{"tmp/development.log"},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}

	app := NewApp(config)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Panic occurred while processing large log file: %v", r)
		}
	}()

	// Process the file
	app.readFromFile("tmp/development.log")

	entries := app.model.buffer.GetAll()
	t.Logf("Successfully processed %d entries from development.log", len(entries))

	// Check that it respects the MaxLines limit
	if len(entries) > 1000 {
		t.Errorf("Expected max 1000 entries due to circular buffer, got %d", len(entries))
	}
}

func TestDebug_ModelInitialization(t *testing.T) {
	// Test if there are any issues with model initialization
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Panic occurred during model initialization: %v", r)
		}
	}()

	model := NewModel(config)

	if model == nil {
		t.Fatal("Model initialization returned nil")
	}

	// Test adding entries
	entry := LogEntry{
		Message:   "Test entry",
		Level:     INFO,
		Timestamp: "2023-12-23 15:30:45",
	}

	model.AddLogEntry(entry)

	if len(model.entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(model.entries))
	}
}

