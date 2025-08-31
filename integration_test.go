package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_FileInput(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")
	
	testData := `2023-12-23 15:30:45 INFO: Regular info message
2023-12-23 15:30:46 ERROR: Something went wrong
2023-12-23 15:30:47 WARN: This is a warning
2023-12-23 15:30:48 DEBUG: Debug information
  (0.5ms)  SELECT "users".* FROM "users" WHERE "users"."id" = $1
  (1.2ms)  INSERT INTO "logs" VALUES ($1, $2)
`
	
	err := os.WriteFile(testFile, []byte(testData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// Create config
	config := &Config{
		MaxLines:    100,
		Files:       []string{testFile},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	// Create app and process the file
	app := NewUnifiedApp(config)
	app.indexFile(testFile)
	
	// Check that entries were parsed
	entries := []LogEntry{}
	if app.model.indexer != nil {
		count := app.model.indexer.LineCount()
		if count > 0 {
			lines := app.model.indexer.GetLines(0, count)
			for _, line := range lines {
				entry := app.model.parser.ParseLogLine(line, testFile)
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		t.Fatal("No entries were parsed from the test file")
	}
	
	// Verify different log levels were detected
	var hasError, hasWarn, hasInfo, hasDebug bool
	for _, entry := range entries {
		switch entry.Level {
		case ERROR:
			hasError = true
		case WARN:
			hasWarn = true
		case INFO:
			hasInfo = true
		case DEBUG:
			hasDebug = true
		}
		
		// Verify source is set
		if entry.Source != testFile {
			t.Errorf("Expected source to be '%s', got '%s'", testFile, entry.Source)
		}
	}
	
	if !hasError {
		t.Error("Expected to find ERROR level logs")
	}
	if !hasWarn {
		t.Error("Expected to find WARN level logs")
	}
	if !hasInfo {
		t.Error("Expected to find INFO level logs")
	}
	if !hasDebug {
		t.Error("Expected to find DEBUG level logs (SQL queries)")
	}
}

func TestIntegration_Filtering(t *testing.T) {
	// Create config with include filter
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "ERROR",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewUnifiedModel(config)
	
	// Add test entries
	entries := []LogEntry{
		{Message: "INFO: Regular message", Level: INFO},
		{Message: "ERROR: Something failed", Level: ERROR},
		{Message: "WARN: A warning", Level: WARN},
		{Message: "ERROR: Another error", Level: ERROR},
	}
	
	for _, entry := range entries {
		model.AddLogEntry(entry)
	}
	
	// Apply filters
	model.applyFilters()
	
	// Apply filters to check results
	model.applyFilters()
	
	// Check filtered results
	if len(model.filteredEntries) != 2 {
		t.Errorf("Expected 2 filtered entries (ERROR only), got %d", len(model.filteredEntries))
	}
	
	// Verify all filtered entries contain "ERROR"
	for _, entry := range model.filteredEntries {
		if !strings.Contains(entry.Message, "ERROR") {
			t.Errorf("Filtered entry should contain 'ERROR': %s", entry.Message)
		}
	}
}

func TestIntegration_LogLevelFiltering(t *testing.T) {
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewUnifiedModel(config)
	
	// Disable INFO level
	model.showInfo = false
	
	// Add test entries
	entries := []LogEntry{
		{Message: "INFO message", Level: INFO},
		{Message: "ERROR message", Level: ERROR},
		{Message: "WARN message", Level: WARN},
		{Message: "DEBUG message", Level: DEBUG},
	}
	
	for _, entry := range entries {
		model.AddLogEntry(entry)
	}
	
	// Apply filters
	model.applyFilters()
	
	// Check that INFO is filtered out
	infoCount := 0
	for _, entry := range model.filteredEntries {
		if entry.Level == INFO {
			infoCount++
		}
	}
	
	if infoCount > 0 {
		t.Errorf("Expected no INFO entries in filtered results, got %d", infoCount)
	}
	
	// Should have ERROR, WARN, DEBUG (3 total)
	if len(model.filteredEntries) != 3 {
		t.Errorf("Expected 3 filtered entries, got %d", len(model.filteredEntries))
	}
}

func TestIntegration_RealLogFile(t *testing.T) {
	// Test with the actual development.log file if it exists and is not too large
	devLogPath := "tmp/small_test.log"
	
	// Check if our small test file exists
	if _, err := os.Stat(devLogPath); os.IsNotExist(err) {
		t.Skip("Small test file not found, skipping integration test with real logs")
	}
	
	config := &Config{
		MaxLines:    1000,
		Files:       []string{devLogPath},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	app := NewUnifiedApp(config)
	app.indexFile(devLogPath)
	
	// Get entries through the model
	entries := []LogEntry{}
	if app.model.indexer != nil {
		count := app.model.indexer.LineCount()
		if count > 0 {
			lines := app.model.indexer.GetLines(0, count)
			for _, line := range lines {
				entry := app.model.parser.ParseLogLine(line, devLogPath)
				entries = append(entries, entry)
			}
		}
	}
	
	if len(entries) == 0 {
		t.Error("No entries were parsed from the real log file")
	}
	
	t.Logf("Parsed %d entries from %s", len(entries), devLogPath)
	
	// Verify we can handle Rails logs with ANSI codes
	railsLogCount := 0
	for _, entry := range entries {
		if entry.Metadata != nil {
			if _, hasDuration := entry.Metadata["duration_ms"]; hasDuration {
				railsLogCount++
			}
		}
	}
	
	if railsLogCount == 0 {
		t.Log("No Rails logs with duration detected (this might be normal depending on log content)")
	} else {
		t.Logf("Detected %d Rails logs with timing information", railsLogCount)
	}
}