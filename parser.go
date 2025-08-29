package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// OTLP log structure (simplified)
type OTLPLog struct {
	Timestamp         int64                  `json:"timeUnixNano"`
	SeverityNumber    int                    `json:"severityNumber"`
	SeverityText      string                 `json:"severityText"`
	Body              interface{}            `json:"body"`
	Attributes        map[string]interface{} `json:"attributes"`
	Resource          map[string]interface{} `json:"resource"`
	InstrumentationScope map[string]interface{} `json:"instrumentationScope"`
}

type LogParser struct {
	timezone *time.Location
}

func NewLogParser(timezone string) *LogParser {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	
	return &LogParser{
		timezone: loc,
	}
}

func (p *LogParser) ParseLogLine(line string, source string) LogEntry {
	// First, try to parse as OTLP JSON
	if entry, ok := p.tryParseOTLP(line); ok {
		entry.Source = source
		return entry
	}
	
	// Try to parse as structured log (Rails, etc.)
	if entry, ok := p.tryParseStructured(line); ok {
		entry.Source = source
		return entry
	}
	
	// Fallback to plain text parsing
	return p.parsePlainText(line, source)
}

func (p *LogParser) tryParseOTLP(line string) (LogEntry, bool) {
	var otlpLog OTLPLog
	if err := json.Unmarshal([]byte(line), &otlpLog); err != nil {
		return LogEntry{}, false
	}
	
	entry := LogEntry{
		Raw:      line,
		Metadata: make(map[string]interface{}),
	}
	
	// Convert timestamp
	if otlpLog.Timestamp > 0 {
		t := time.Unix(0, otlpLog.Timestamp).In(p.timezone)
		entry.Timestamp = t.Format("2006-01-02 15:04:05")
	} else {
		entry.Timestamp = time.Now().In(p.timezone).Format("2006-01-02 15:04:05")
	}
	
	// Convert severity
	entry.Level = p.otlpSeverityToLevel(otlpLog.SeverityNumber, otlpLog.SeverityText)
	
	// Extract message
	if body, ok := otlpLog.Body.(string); ok {
		entry.Message = body
	} else if bodyMap, ok := otlpLog.Body.(map[string]interface{}); ok {
		if msg, exists := bodyMap["stringValue"]; exists {
			entry.Message = msg.(string)
		} else {
			bodyBytes, _ := json.Marshal(otlpLog.Body)
			entry.Message = string(bodyBytes)
		}
	}
	
	// Store metadata
	if otlpLog.Attributes != nil {
		entry.Metadata["attributes"] = otlpLog.Attributes
	}
	if otlpLog.Resource != nil {
		entry.Metadata["resource"] = otlpLog.Resource
	}
	if otlpLog.InstrumentationScope != nil {
		entry.Metadata["instrumentationScope"] = otlpLog.InstrumentationScope
	}
	
	return entry, true
}

func (p *LogParser) tryParseStructured(line string) (LogEntry, bool) {
	// Remove ANSI codes for parsing
	cleanLine := ansiRegex.ReplaceAllString(line, "")
	
	// Try Rails log format: "  (0.3ms)  SQL query" (after ANSI stripping)
	railsRegex := regexp.MustCompile(`^\s*\(([0-9.]+)ms\)\s+(.+)$`)
	if matches := railsRegex.FindStringSubmatch(cleanLine); len(matches) == 3 {
		entry := LogEntry{
			Timestamp: time.Now().In(p.timezone).Format("2006-01-02 15:04:05"),
			Level:     INFO,
			Message:   matches[2],
			Raw:       line,
			Metadata:  map[string]interface{}{
				"duration_ms": matches[1],
			},
		}
		
		// Detect level based on content
		upperMsg := strings.ToUpper(entry.Message)
		if strings.Contains(upperMsg, "ERROR") {
			entry.Level = ERROR
		} else if strings.Contains(upperMsg, "WARN") {
			entry.Level = WARN
		} else if strings.Contains(upperMsg, "SQL") || strings.Contains(upperMsg, "SELECT") || strings.Contains(upperMsg, "INSERT") || strings.Contains(upperMsg, "UPDATE") || strings.Contains(upperMsg, "DELETE") {
			entry.Level = DEBUG
		}
		
		return entry, true
	}
	
	// Try common log formats like Apache/Nginx
	commonLogRegex := regexp.MustCompile(`^(\S+) - - \[([^\]]+)\] "([^"]*)" (\d+) (\d+)`)
	if matches := commonLogRegex.FindStringSubmatch(cleanLine); len(matches) == 6 {
		entry := LogEntry{
			Timestamp: matches[2], // TODO: parse this properly
			Level:     INFO,
			Message:   fmt.Sprintf("%s %s - Status: %s", matches[1], matches[3], matches[4]),
			Raw:       line,
			Metadata: map[string]interface{}{
				"ip":           matches[1],
				"request":      matches[3],
				"status_code":  matches[4],
				"response_size": matches[5],
			},
		}
		
		// Determine level based on status code
		if statusCode, err := strconv.Atoi(matches[4]); err == nil {
			if statusCode >= 500 {
				entry.Level = ERROR
			} else if statusCode >= 400 {
				entry.Level = WARN
			}
		}
		
		return entry, true
	}
	
	return LogEntry{}, false
}

func (p *LogParser) parsePlainText(line string, source string) LogEntry {
	// Clean ANSI codes for message display
	cleanLine := ansiRegex.ReplaceAllString(line, "")
	
	entry := LogEntry{
		Timestamp: time.Now().In(p.timezone).Format("2006-01-02 15:04:05"),
		Level:     INFO,
		Message:   cleanLine,
		Raw:       line,
		Source:    source,
		Metadata:  make(map[string]interface{}),
	}
	
	// Try to detect log level from the line
	upperLine := strings.ToUpper(cleanLine)
	if strings.Contains(upperLine, "ERROR") || strings.Contains(upperLine, "FATAL") {
		entry.Level = ERROR
	} else if strings.Contains(upperLine, "WARN") || strings.Contains(upperLine, "WARNING") {
		entry.Level = WARN
	} else if strings.Contains(upperLine, "DEBUG") || strings.Contains(upperLine, "TRACE") {
		entry.Level = DEBUG
	}
	
	// Try to extract timestamp from common formats
	p.extractTimestamp(&entry, cleanLine)
	
	return entry
}

func (p *LogParser) extractTimestamp(entry *LogEntry, line string) {
	// Common timestamp patterns
	patterns := []string{
		`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`,                    // 2023-01-01 12:00:00
		`(\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2})`,                   // 01/Jan/2023:12:00:00
		`(\w{3} \d{1,2} \d{2}:\d{2}:\d{2})`,                       // Jan 1 12:00:00
		`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))`, // ISO 8601
	}
	
	for _, pattern := range patterns {
		regex := regexp.MustCompile(pattern)
		if matches := regex.FindStringSubmatch(line); len(matches) > 1 {
			// Try to parse the timestamp
			formats := []string{
				"2006-01-02 15:04:05",
				"02/Jan/2006:15:04:05",
				"Jan 2 15:04:05",
				time.RFC3339,
				time.RFC3339Nano,
			}
			
			for _, format := range formats {
				if t, err := time.Parse(format, matches[1]); err == nil {
					entry.Timestamp = t.In(p.timezone).Format("2006-01-02 15:04:05")
					return
				}
			}
		}
	}
}

func (p *LogParser) otlpSeverityToLevel(severityNumber int, severityText string) LogLevel {
	// OTLP severity numbers: https://opentelemetry.io/docs/reference/specification/logs/data-model/#severity-fields
	switch {
	case severityNumber >= 17: // ERROR and above
		return ERROR
	case severityNumber >= 13: // WARN and above
		return WARN
	case severityNumber >= 9:  // INFO and above
		return INFO
	case severityNumber >= 5:  // DEBUG and above
		return DEBUG
	default:
		// Fall back to severity text
		switch strings.ToUpper(severityText) {
		case "ERROR", "FATAL":
			return ERROR
		case "WARN", "WARNING":
			return WARN
		case "INFO":
			return INFO
		case "DEBUG", "TRACE":
			return DEBUG
		default:
			return INFO
		}
	}
}

func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}