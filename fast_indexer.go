package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// FastLineIndex stores just the offset - no parsing at all
type FastLineIndex struct {
	Offset int64  // Byte offset in file
	Length int    // Line length in bytes
}

// FastIndexer does absolutely minimal work during indexing
type FastIndexer struct {
	filename    string
	file        *os.File
	indices     []FastLineIndex
	totalLines  int32 // Use atomic for thread-safe updates
	indexed     bool
	indexMutex  sync.RWMutex
	
	// Cache for parsed entries - larger for better performance
	cache       map[int]LogEntry
	cacheMutex  sync.RWMutex
	cacheSize   int
	
	parser      *LogParser
}

func NewFastIndexer(filename string, parser *LogParser) (*FastIndexer, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	
	// Get file size to pre-allocate slice
	stat, _ := file.Stat()
	estimatedLines := int(stat.Size() / 100) // Estimate ~100 bytes per line
	
	return &FastIndexer{
		filename:  filename,
		file:      file,
		indices:   make([]FastLineIndex, 0, estimatedLines),
		cache:     make(map[int]LogEntry),
		cacheSize: 5000, // Larger cache for better performance
		parser:    parser,
	}, nil
}

// IndexFileUltraFast scans the file with minimal overhead
func (fi *FastIndexer) IndexFileUltraFast() error {
	fi.indexMutex.Lock()
	defer fi.indexMutex.Unlock()
	
	if fi.indexed {
		return nil
	}
	
	// Use larger buffer for better I/O performance
	const bufferSize = 256 * 1024 // 256KB buffer
	buffer := make([]byte, bufferSize)
	
	var offset int64 = 0
	var lineStart int64 = 0
	var lineCount int32 = 0
	
	for {
		n, err := fi.file.Read(buffer)
		if n > 0 {
			// Find all newlines in the buffer
			for i := 0; i < n; i++ {
				if buffer[i] == '\n' {
					lineLen := int(offset + int64(i) - lineStart + 1)
					fi.indices = append(fi.indices, FastLineIndex{
						Offset: lineStart,
						Length: lineLen,
					})
					lineStart = offset + int64(i) + 1
					lineCount++
				}
			}
			offset += int64(n)
		}
		
		if err == io.EOF {
			// Handle last line if no trailing newline
			if lineStart < offset {
				fi.indices = append(fi.indices, FastLineIndex{
					Offset: lineStart,
					Length: int(offset - lineStart),
				})
				lineCount++
			}
			break
		} else if err != nil {
			return err
		}
	}
	
	atomic.StoreInt32(&fi.totalLines, lineCount)
	fi.indexed = true
	
	// Reset file position for reading
	fi.file.Seek(0, 0)
	
	return nil
}

// GetLineRange retrieves multiple lines efficiently in a single read
func (fi *FastIndexer) GetLineRange(start, end int) ([]LogEntry, error) {
	if start < 0 {
		start = 0
	}
	
	total := int(atomic.LoadInt32(&fi.totalLines))
	if end > total {
		end = total
	}
	
	if start >= end {
		return []LogEntry{}, nil
	}
	
	entries := make([]LogEntry, 0, end-start)
	
	// Check cache first
	uncachedRanges := []int{}
	fi.cacheMutex.RLock()
	for i := start; i < end; i++ {
		if entry, ok := fi.cache[i]; ok {
			entries = append(entries, entry)
		} else {
			uncachedRanges = append(uncachedRanges, i)
		}
	}
	fi.cacheMutex.RUnlock()
	
	// If all cached, return immediately
	if len(uncachedRanges) == 0 {
		return entries, nil
	}
	
	// Read uncached lines in batch for efficiency
	fi.indexMutex.RLock()
	defer fi.indexMutex.RUnlock()
	
	if len(uncachedRanges) > 0 && len(fi.indices) > 0 {
		// Calculate total buffer size needed
		totalSize := 0
		for _, idx := range uncachedRanges {
			if idx < len(fi.indices) {
				totalSize += fi.indices[idx].Length
			}
		}
		
		// Read all needed data in one go if consecutive
		if len(uncachedRanges) > 1 && uncachedRanges[len(uncachedRanges)-1] - uncachedRanges[0] == len(uncachedRanges)-1 {
			// Consecutive range - read in one shot
			firstIdx := uncachedRanges[0]
			lastIdx := uncachedRanges[len(uncachedRanges)-1]
			
			if firstIdx < len(fi.indices) && lastIdx < len(fi.indices) {
				startOffset := fi.indices[firstIdx].Offset
				endOffset := fi.indices[lastIdx].Offset + int64(fi.indices[lastIdx].Length)
				bufferSize := int(endOffset - startOffset)
				
				buffer := make([]byte, bufferSize)
				_, err := fi.file.ReadAt(buffer, startOffset)
				if err != nil && err != io.EOF {
					return entries, err
				}
				
				// Parse each line from the buffer
				newEntries := make([]LogEntry, 0, len(uncachedRanges))
				reader := bytes.NewReader(buffer)
				scanner := bufio.NewScanner(reader)
				
				for i, lineIdx := range uncachedRanges {
					if scanner.Scan() {
						line := scanner.Text()
						entry := fi.parser.ParseLogLine(line, fi.filename)
						newEntries = append(newEntries, entry)
						
						// Update cache
						fi.cacheMutex.Lock()
						fi.cache[lineIdx] = entry
						fi.cacheMutex.Unlock()
					}
					_ = i // Use i to avoid warning
				}
				
				entries = append(entries, newEntries...)
			}
		} else {
			// Non-consecutive - read individually (less efficient but needed)
			for _, idx := range uncachedRanges {
				if idx < len(fi.indices) {
					index := fi.indices[idx]
					buffer := make([]byte, index.Length)
					_, err := fi.file.ReadAt(buffer, index.Offset)
					if err != nil && err != io.EOF {
						continue
					}
					
					line := string(buffer)
					if len(line) > 0 && line[len(line)-1] == '\n' {
						line = line[:len(line)-1]
					}
					
					entry := fi.parser.ParseLogLine(line, fi.filename)
					entries = append(entries, entry)
					
					// Update cache
					fi.cacheMutex.Lock()
					fi.cache[idx] = entry
					
					// Simple cache eviction if too large
					if len(fi.cache) > fi.cacheSize {
						// Remove some old entries
						removed := 0
						for k := range fi.cache {
							if k < idx-fi.cacheSize/2 || k > idx+fi.cacheSize/2 {
								delete(fi.cache, k)
								removed++
								if removed > fi.cacheSize/4 {
									break
								}
							}
						}
					}
					fi.cacheMutex.Unlock()
				}
			}
		}
	}
	
	return entries, nil
}

// GetLineCount returns total indexed lines
func (fi *FastIndexer) GetLineCount() int {
	return int(atomic.LoadInt32(&fi.totalLines))
}

// Close releases resources
func (fi *FastIndexer) Close() error {
	return fi.file.Close()
}