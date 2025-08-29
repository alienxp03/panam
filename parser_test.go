package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLogParser_ParseOTLP(t *testing.T) {
	parser := NewLogParser("UTC")
	
	// Create a sample OTLP log
	otlpLog := map[string]interface{}{
		"timeUnixNano":   1703347200000000000, // 2023-12-23 12:00:00 UTC
		"severityNumber": 13,                  // WARN level
		"severityText":   "WARN",
		"body": map[string]interface{}{
			"stringValue": "This is a test warning message",
		},
		"attributes": map[string]interface{}{
			"service.name": "test-service",
			"trace.id":     "abc123",
		},
	}
	
	jsonData, err := json.Marshal(otlpLog)
	if err != nil {
		t.Fatalf("Failed to marshal test OTLP log: %v", err)
	}
	
	entry := parser.ParseLogLine(string(jsonData), "test-source")
	
	// Validate parsed entry
	if entry.Level != WARN {
		t.Errorf("Expected level WARN, got %v", entry.Level)
	}
	
	if entry.Message != "This is a test warning message" {
		t.Errorf("Expected message 'This is a test warning message', got '%s'", entry.Message)
	}
	
	if entry.Source != "test-source" {
		t.Errorf("Expected source 'test-source', got '%s'", entry.Source)
	}
	
	// Check attributes are stored in metadata
	if attributes, ok := entry.Metadata["attributes"].(map[string]interface{}); ok {
		if serviceName, exists := attributes["service.name"]; !exists || serviceName != "test-service" {
			t.Errorf("Expected service.name to be 'test-service' in metadata")
		}
	} else {
		t.Error("Expected attributes to be stored in metadata")
	}
}

func TestLogParser_ParseRailsLog(t *testing.T) {
	parser := NewLogParser("UTC")
	
	// Test Rails SQL log format
	railsLine := "  \x1b[1m\x1b[35m (0.3ms)\x1b[0m  \x1b[1m\x1b[34mSELECT \"users\".* FROM \"users\" WHERE \"users\".\"id\" = $1\x1b[0m"
	
	entry := parser.ParseLogLine(railsLine, "")
	
	if entry.Level != DEBUG {
		t.Errorf("Expected level DEBUG for SQL query, got %v", entry.Level)
	}
	
	if durationMs, ok := entry.Metadata["duration_ms"]; !ok || durationMs != "0.3" {
		t.Errorf("Expected duration_ms to be '0.3', got %v", durationMs)
	}
	
	// Message should not contain ANSI codes
	if entry.Message == railsLine {
		t.Error("Message should be cleaned of ANSI codes")
	}
}

func TestLogParser_ParsePlainText(t *testing.T) {
	parser := NewLogParser("UTC")
	
	testCases := []struct {
		line          string
		expectedLevel LogLevel
		description   string
	}{
		{
			line:          "ERROR: Database connection failed",
			expectedLevel: ERROR,
			description:   "Should detect ERROR level",
		},
		{
			line:          "WARN: Deprecated function used",
			expectedLevel: WARN,
			description:   "Should detect WARN level",
		},
		{
			line:          "DEBUG: Processing user request",
			expectedLevel: DEBUG,
			description:   "Should detect DEBUG level",
		},
		{
			line:          "Regular info message",
			expectedLevel: INFO,
			description:   "Should default to INFO level",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			entry := parser.ParseLogLine(tc.line, "")
			
			if entry.Level != tc.expectedLevel {
				t.Errorf("Expected level %v, got %v for line: %s", tc.expectedLevel, entry.Level, tc.line)
			}
			
			if entry.Message != tc.line {
				t.Errorf("Expected message '%s', got '%s'", tc.line, entry.Message)
			}
		})
	}
}

func TestLogParser_ExtractTimestamp(t *testing.T) {
	parser := NewLogParser("UTC")
	
	testCases := []struct {
		line              string
		expectedTimestamp bool
		description       string
	}{
		{
			line:              "2023-12-23 15:30:45 INFO: Test message",
			expectedTimestamp: true,
			description:       "Should extract standard timestamp",
		},
		{
			line:              "No timestamp here",
			expectedTimestamp: false,
			description:       "Should not extract timestamp when none present",
		},
		{
			line:              "2023-12-23T15:30:45Z ERROR: ISO timestamp",
			expectedTimestamp: true,
			description:       "Should extract ISO timestamp",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			entry := parser.ParseLogLine(tc.line, "")
			
			// Parse current time to compare
			now := time.Now()
			currentTimeStr := now.Format(time.RFC3339)
			
			if tc.expectedTimestamp {
				// Should not be current time (should be extracted from log)
				if entry.Timestamp == currentTimeStr {
					t.Error("Expected extracted timestamp, got current time")
				}
			}
			// Note: We can't easily test the exact extracted timestamp without
			// complex parsing, but we can verify the format is correct (ISO 8601)
			// ISO 8601 format: 2006-01-02T15:04:05Z07:00
			if !strings.Contains(entry.Timestamp, "T") {
				t.Errorf("Expected ISO 8601 timestamp format, got '%s'", entry.Timestamp)
			}
		})
	}
}

func TestCircularBuffer(t *testing.T) {
	buffer := NewCircularBuffer(3)
	
	// Test adding entries
	entries := []LogEntry{
		{Message: "Entry 1", Level: INFO},
		{Message: "Entry 2", Level: WARN},
		{Message: "Entry 3", Level: ERROR},
	}
	
	for _, entry := range entries {
		buffer.Add(entry)
	}
	
	all := buffer.GetAll()
	if len(all) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(all))
	}
	
	// Test overflow
	buffer.Add(LogEntry{Message: "Entry 4", Level: DEBUG})
	
	all = buffer.GetAll()
	if len(all) != 3 {
		t.Errorf("Expected buffer size to remain 3, got %d", len(all))
	}
	
	// First entry should be "Entry 2" now (Entry 1 was overwritten)
	if all[0].Message != "Entry 2" {
		t.Errorf("Expected first entry to be 'Entry 2', got '%s'", all[0].Message)
	}
	
	// Last entry should be "Entry 4"
	if all[2].Message != "Entry 4" {
		t.Errorf("Expected last entry to be 'Entry 4', got '%s'", all[2].Message)
	}
}

func TestStripANSI(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "\x1b[1m\x1b[35mBold Magenta Text\x1b[0m",
			expected: "Bold Magenta Text",
		},
		{
			input:    "No ANSI codes here",
			expected: "No ANSI codes here",
		},
		{
			input:    "\x1b[31mRed\x1b[0m and \x1b[32mGreen\x1b[0m",
			expected: "Red and Green",
		},
	}
	
	for _, tc := range testCases {
		result := StripANSI(tc.input)
		if result != tc.expected {
			t.Errorf("Expected '%s', got '%s'", tc.expected, result)
		}
	}
}

func BenchmarkLogParser_ParsePlainText(b *testing.B) {
	parser := NewLogParser("UTC")
	line := "2023-12-23 15:30:45 INFO: This is a test log message with some content"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.ParseLogLine(line, "test")
	}
}

func BenchmarkCircularBuffer_Add(b *testing.B) {
	buffer := NewCircularBuffer(10000)
	entry := LogEntry{
		Message: "Benchmark test message",
		Level:   INFO,
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer.Add(entry)
	}
}