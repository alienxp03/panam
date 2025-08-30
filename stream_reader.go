package main

import (
	"bufio"
	"io"
	"os"
	"sync"
	"time"
	
	tea "github.com/charmbracelet/bubbletea"
)

// StreamReader handles different input sources efficiently
type StreamReader struct {
	config     *Config
	parser     *LogParser
	program    *tea.Program
	
	// For file-based reading
	indexers   map[string]*FileIndexer
	
	// For streaming (stdin/tail)
	streamBuf  []LogEntry
	streamMux  sync.Mutex
}

func NewStreamReader(config *Config, parser *LogParser, program *tea.Program) *StreamReader {
	return &StreamReader{
		config:   config,
		parser:   parser,
		program:  program,
		indexers: make(map[string]*FileIndexer),
	}
}

// ProcessInput determines input type and processes accordingly
func (sr *StreamReader) ProcessInput() error {
	// Check if we have piped input
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		// Piped input - use streaming mode
		return sr.streamFromStdin()
	}
	
	// Process files if specified
	if len(sr.config.Files) > 0 {
		return sr.processFiles()
	}
	
	return nil
}

// processFiles handles file-based input with indexing
func (sr *StreamReader) processFiles() error {
	for _, filename := range sr.config.Files {
		// Check file size to determine strategy
		info, err := os.Stat(filename)
		if err != nil {
			continue
		}
		
		// For large files (>10MB), use indexing
		if info.Size() > 10*1024*1024 {
			go sr.indexLargeFile(filename)
		} else {
			// For small files, use traditional loading
			go sr.loadSmallFile(filename)
		}
	}
	
	return nil
}

// indexLargeFile efficiently indexes large files
func (sr *StreamReader) indexLargeFile(filename string) {
	indexer, err := NewFileIndexer(filename, sr.parser)
	if err != nil {
		return
	}
	
	sr.indexers[filename] = indexer
	
	// Start indexing in background
	go func() {
		startTime := time.Now()
		
		// Send initial status
		if sr.program != nil {
			sr.program.Send(IndexStatusMsg{
				Filename: filename,
				Status:   "Indexing...",
				Progress: 0,
			})
		}
		
		// Index the file
		if err := indexer.IndexFile(); err != nil {
			return
		}
		
		indexTime := time.Since(startTime)
		
		// Send completion status
		if sr.program != nil {
			sr.program.Send(IndexStatusMsg{
				Filename: filename,
				Status:   "Indexed",
				Progress: 100,
				Lines:    indexer.GetLineCount(),
				Duration: indexTime,
			})
			
			// Send initial visible window
			sr.sendVisibleWindow(indexer, 0, 100)
		}
	}()
}

// loadSmallFile loads small files traditionally
func (sr *StreamReader) loadSmallFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	
	batch := make([]LogEntry, 0, 100)
	lastSend := time.Now()
	
	for scanner.Scan() {
		line := scanner.Text()
		entry := sr.parser.ParseLogLine(line, filename)
		batch = append(batch, entry)
		
		if len(batch) >= 100 || time.Since(lastSend) > 20*time.Millisecond {
			sr.sendBatch(batch)
			batch = batch[:0]
			lastSend = time.Now()
		}
	}
	
	if len(batch) > 0 {
		sr.sendBatch(batch)
	}
}

// streamFromStdin handles piped input
func (sr *StreamReader) streamFromStdin() error {
	reader := bufio.NewReaderSize(os.Stdin, 128*1024) // 128KB buffer
	batch := make([]LogEntry, 0, 200)
	lastSend := time.Now()
	
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		
		if len(line) > 0 {
			entry := sr.parser.ParseLogLine(line, "stdin")
			batch = append(batch, entry)
			
			// Send larger batches for better performance
			if len(batch) >= 200 || time.Since(lastSend) > 30*time.Millisecond {
				sr.sendBatch(batch)
				batch = batch[:0]
				lastSend = time.Now()
			}
		}
		
		if err == io.EOF {
			break
		}
	}
	
	if len(batch) > 0 {
		sr.sendBatch(batch)
	}
	
	return nil
}

// sendVisibleWindow sends only the visible portion of indexed data
func (sr *StreamReader) sendVisibleWindow(indexer *FileIndexer, start, count int) {
	entries, err := indexer.GetLines(start, start+count)
	if err != nil {
		return
	}
	
	if sr.program != nil && len(entries) > 0 {
		sr.program.Send(LogWindowMsg{
			Entries:    entries,
			TotalLines: indexer.GetLineCount(),
			StartLine:  start,
		})
	}
}

// sendBatch sends a batch of log entries
func (sr *StreamReader) sendBatch(entries []LogEntry) {
	if sr.program != nil && len(entries) > 0 {
		sr.program.Send(LogBatchMsg(entries))
	}
}

// GetIndexer returns the indexer for a file
func (sr *StreamReader) GetIndexer(filename string) *FileIndexer {
	return sr.indexers[filename]
}