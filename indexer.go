package main

import (
	"bufio"
	"io"
	"os"
	"sync"
)

// LineIndex stores metadata about lines without loading content
type LineIndex struct {
	Offset int64  // Byte offset in file
	Length int    // Line length in bytes
	Level  LogLevel // Quick level detection
}

// FileIndexer efficiently indexes large log files
type FileIndexer struct {
	filename    string
	file        *os.File
	indices     []LineIndex
	totalLines  int
	indexed     bool
	indexMutex  sync.RWMutex
	
	// Cache for parsed entries
	cache       map[int]LogEntry
	cacheMutex  sync.RWMutex
	cacheSize   int
	
	parser      *LogParser
}

func NewFileIndexer(filename string, parser *LogParser) (*FileIndexer, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	
	return &FileIndexer{
		filename:  filename,
		file:      file,
		indices:   make([]LineIndex, 0, 10000),
		cache:     make(map[int]LogEntry),
		cacheSize: 1000, // Cache last 1000 parsed entries
		parser:    parser,
	}, nil
}

// IndexFile quickly scans the file to build line index
func (fi *FileIndexer) IndexFile() error {
	fi.indexMutex.Lock()
	defer fi.indexMutex.Unlock()
	
	if fi.indexed {
		return nil
	}
	
	// Reset file position
	fi.file.Seek(0, 0)
	reader := bufio.NewReaderSize(fi.file, 64*1024) // 64KB buffer
	
	var offset int64 = 0
	lineNum := 0
	
	for {
		lineStart := offset
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return err
		}
		
		if len(line) > 0 {
			// Quick level detection without full parsing
			level := fi.quickDetectLevel(string(line))
			
			fi.indices = append(fi.indices, LineIndex{
				Offset: lineStart,
				Length: len(line),
				Level:  level,
			})
			
			offset += int64(len(line))
			lineNum++
		}
		
		if err == io.EOF {
			break
		}
	}
	
	fi.totalLines = lineNum
	fi.indexed = true
	return nil
}

// quickDetectLevel does minimal parsing for performance
func (fi *FileIndexer) quickDetectLevel(line string) LogLevel {
	// Quick scan for log level keywords
	if len(line) > 20 { // Skip very short lines
		// Check common positions for log levels
		if contains(line[:min(100, len(line))], "ERROR", "FATAL", "error", "fatal") {
			return ERROR
		}
		if contains(line[:min(100, len(line))], "WARN", "WARNING", "warn", "warning") {
			return WARN
		}
		if contains(line[:min(100, len(line))], "DEBUG", "TRACE", "debug", "trace") {
			return DEBUG
		}
	}
	return INFO
}

// GetLine retrieves and parses a specific line
func (fi *FileIndexer) GetLine(lineNum int) (LogEntry, error) {
	if lineNum < 0 || lineNum >= len(fi.indices) {
		return LogEntry{}, nil
	}
	
	// Check cache first
	fi.cacheMutex.RLock()
	if entry, ok := fi.cache[lineNum]; ok {
		fi.cacheMutex.RUnlock()
		return entry, nil
	}
	fi.cacheMutex.RUnlock()
	
	// Read line from file
	fi.indexMutex.RLock()
	index := fi.indices[lineNum]
	fi.indexMutex.RUnlock()
	
	// Seek to line position
	buffer := make([]byte, index.Length)
	_, err := fi.file.ReadAt(buffer, index.Offset)
	if err != nil {
		return LogEntry{}, err
	}
	
	// Parse the line
	line := string(buffer)
	entry := fi.parser.ParseLogLine(line, fi.filename)
	
	// Update cache
	fi.cacheMutex.Lock()
	fi.cache[lineNum] = entry
	// Simple cache eviction if too large
	if len(fi.cache) > fi.cacheSize {
		// Remove oldest entries (simple strategy)
		for k := range fi.cache {
			delete(fi.cache, k)
			if len(fi.cache) <= fi.cacheSize/2 {
				break
			}
		}
	}
	fi.cacheMutex.Unlock()
	
	return entry, nil
}

// GetLines retrieves multiple lines efficiently
func (fi *FileIndexer) GetLines(start, end int) ([]LogEntry, error) {
	if start < 0 {
		start = 0
	}
	if end > len(fi.indices) {
		end = len(fi.indices)
	}
	
	entries := make([]LogEntry, 0, end-start)
	for i := start; i < end; i++ {
		entry, err := fi.GetLine(i)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	
	return entries, nil
}

// GetLineCount returns total indexed lines
func (fi *FileIndexer) GetLineCount() int {
	fi.indexMutex.RLock()
	defer fi.indexMutex.RUnlock()
	return fi.totalLines
}

// FilteredSearch performs efficient searching with level filtering
func (fi *FileIndexer) FilteredSearch(includePattern, excludePattern string, levels []LogLevel) []int {
	results := make([]int, 0)
	
	fi.indexMutex.RLock()
	defer fi.indexMutex.RUnlock()
	
	// Quick level filtering using index
	levelMap := make(map[LogLevel]bool)
	for _, level := range levels {
		levelMap[level] = true
	}
	
	for i, index := range fi.indices {
		// Quick level check from index
		if !levelMap[index.Level] {
			continue
		}
		
		// For pattern matching, we need to read the line
		// But we can batch these reads for efficiency
		results = append(results, i)
	}
	
	return results
}

// Close releases resources
func (fi *FileIndexer) Close() error {
	return fi.file.Close()
}

// Helper function for quick string search
func contains(s string, substrs ...string) bool {
	for _, substr := range substrs {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}