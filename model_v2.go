package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// VirtualModel implements virtual scrolling for performance
type VirtualModel struct {
	config *Config
	
	// Virtual scrolling state
	visibleEntries  []LogEntry  // Only visible entries
	totalLines      int         // Total lines in source
	viewportStart   int         // First visible line index
	viewportHeight  int         // Number of visible lines
	
	// Indexers for file sources
	indexers        map[string]*FileIndexer
	activeFile      string
	
	// Streaming buffer for stdin
	streamBuffer    []LogEntry
	
	// UI state
	focus          PanelFocus
	width          int
	height         int
	selectedIdx    int  // Relative to viewport
	absoluteIdx    int  // Absolute line number
	viewMode       ViewMode
	leftWidth      int
	rightWidth     int
	tailing        bool
	lastGPress     int64 // For detecting "gg" key combo
	
	// Filter inputs
	includeInput   textinput.Model
	excludeInput   textinput.Model
	activeInput    *textinput.Model
	useRegex       bool
	caseSensitive  bool
	
	// Left panel navigation
	leftPanelItem  int
	editMode       bool
	
	// Log level filters
	showDebug      bool
	showInfo       bool
	showWarn       bool
	showError      bool
	
	// Status
	indexStatus    string
	loadProgress   float64
	
	// Styles
	focusedStyle   lipgloss.Style
	blurredStyle   lipgloss.Style
	selectedStyle  lipgloss.Style
	headerStyle    lipgloss.Style
	levelStyles    map[LogLevel]lipgloss.Style
	
	// Reader for efficient input handling
	reader         *StreamReader
	
	mutex          sync.RWMutex
}

// Message types for virtual scrolling
type IndexStatusMsg struct {
	Filename string
	Status   string
	Progress float64
	Lines    int
	Duration time.Duration
}

type LogWindowMsg struct {
	Entries    []LogEntry
	TotalLines int
	StartLine  int
}

type ScrollRequestMsg struct {
	Start int
	Count int
}

func NewVirtualModel(config *Config) *VirtualModel {
	includeInput := textinput.New()
	includeInput.Placeholder = "Include patterns (comma-separated)"
	includeInput.CharLimit = 256
	if config.Include != "" {
		includeInput.SetValue(config.Include)
	}

	excludeInput := textinput.New()
	excludeInput.Placeholder = "Exclude patterns (comma-separated)"
	excludeInput.CharLimit = 256
	if config.Exclude != "" {
		excludeInput.SetValue(config.Exclude)
	}

	m := &VirtualModel{
		config:         config,
		visibleEntries: make([]LogEntry, 0),
		streamBuffer:   make([]LogEntry, 0, 1000),
		indexers:       make(map[string]*FileIndexer),
		focus:          RightPanel,
		viewMode:       NormalView,
		showDebug:      true,
		showInfo:       true,
		showWarn:       true,
		showError:      true,
		includeInput:   includeInput,
		excludeInput:   excludeInput,
		viewportHeight: 40, // Default viewport
		tailing:        true,
		indexStatus:    "Ready",
	}

	// Initialize styles
	m.focusedStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("69"))

	m.blurredStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))

	m.selectedStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("235"))

	m.headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))

	m.levelStyles = map[LogLevel]lipgloss.Style{
		DEBUG: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		INFO:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		WARN:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		ERROR: lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}

	return m
}

func (m *VirtualModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
		tickCmd(),
	)
}

func (m *VirtualModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewportHeight = m.height - 10 // Account for UI chrome
		
		// Calculate panel widths
		m.leftWidth = m.width * 30 / 100
		if m.leftWidth < 25 {
			m.leftWidth = 25
		}
		if m.leftWidth > 40 {
			m.leftWidth = 40
		}
		m.rightWidth = m.width - m.leftWidth
		
		// Request data for new viewport
		if m.activeFile != "" && m.indexers[m.activeFile] != nil {
			return m, m.requestVisibleData()
		}
		
		return m, nil

	case IndexStatusMsg:
		m.indexStatus = msg.Status
		m.loadProgress = msg.Progress
		if msg.Lines > 0 {
			m.totalLines = msg.Lines
		}
		if msg.Status == "Indexed" {
			// Request initial data
			m.activeFile = msg.Filename
			return m, m.requestVisibleData()
		}
		return m, nil

	case LogWindowMsg:
		m.mutex.Lock()
		m.visibleEntries = msg.Entries
		m.totalLines = msg.TotalLines
		m.viewportStart = msg.StartLine
		m.mutex.Unlock()
		return m, nil

	case LogBatchMsg:
		// For streaming mode
		m.mutex.Lock()
		m.streamBuffer = append(m.streamBuffer, []LogEntry(msg)...)
		// Keep only last N entries for memory efficiency
		if len(m.streamBuffer) > m.config.MaxLines {
			m.streamBuffer = m.streamBuffer[len(m.streamBuffer)-m.config.MaxLines:]
		}
		m.totalLines = len(m.streamBuffer)
		
		// Update visible entries if in streaming mode
		if m.activeFile == "" {
			m.updateVisibleFromStream()
		}
		m.mutex.Unlock()
		return m, nil

	case tickMsg:
		return m, tickCmd()

	case tea.KeyMsg:
		// Handle navigation
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
			
		case "j", "down":
			if m.focus == RightPanel {
				m.scrollDown()
				return m, m.requestVisibleData()
			}
			
		case "k", "up":
			if m.focus == RightPanel {
				m.scrollUp()
				return m, m.requestVisibleData()
			}
			
		case "ctrl+d":
			// Half page down
			m.scrollHalfPageDown()
			return m, m.requestVisibleData()
			
		case "ctrl+u":
			// Half page up
			m.scrollHalfPageUp()
			return m, m.requestVisibleData()
			
		case "G":
			// Go to bottom
			m.scrollToBottom()
			return m, m.requestVisibleData()
			
		case "g":
			// Check for gg
			now := time.Now().UnixNano()
			if now-m.lastGPress < 500000000 { // 500ms
				m.scrollToTop()
				return m, m.requestVisibleData()
			}
			m.lastGPress = now
			return m, nil
			
		case "tab":
			// Toggle focus
			if m.focus == LeftPanel {
				m.focus = RightPanel
			} else {
				m.focus = LeftPanel
			}
			return m, nil
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *VirtualModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Build left panel
	leftPanel := m.renderLeftPanel()
	
	// Build right panel
	rightPanel := m.renderRightPanel()

	// Join panels
	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Add header
	header := m.renderHeader()

	// Combine everything
	return lipgloss.JoinVertical(lipgloss.Left, header, panels)
}

func (m *VirtualModel) renderHeader() string {
	title := " Panam Log Viewer "
	
	// Show loading progress if indexing
	status := fmt.Sprintf("Lines: %d", m.totalLines)
	if m.indexStatus == "Indexing..." {
		status = fmt.Sprintf("Indexing... %.0f%%", m.loadProgress)
	}
	
	liveIndicator := ""
	if m.tailing {
		liveIndicator = " | Live ●"
	}
	
	headerText := fmt.Sprintf("%s%s%s%s",
		title,
		strings.Repeat(" ", m.width-len(title)-len(status)-len(liveIndicator)),
		status,
		liveIndicator)
	
	return m.headerStyle.Width(m.width).Render(headerText)
}

func (m *VirtualModel) renderLeftPanel() string {
	var content strings.Builder
	
	content.WriteString("🔍 SEARCH & FILTERS\n\n")
	
	// Show active file if any
	if m.activeFile != "" {
		content.WriteString("📁 File: " + m.activeFile + "\n\n")
	}
	
	// Include filter
	content.WriteString("  Include Pattern:\n")
	if m.leftPanelItem == 0 && m.focus == LeftPanel {
		content.WriteString("▶ ")
	} else {
		content.WriteString("  ")
	}
	content.WriteString(m.includeInput.View() + "\n\n")
	
	// Exclude filter
	content.WriteString("  Exclude Pattern:\n")
	if m.leftPanelItem == 1 && m.focus == LeftPanel {
		content.WriteString("▶ ")
	} else {
		content.WriteString("  ")
	}
	content.WriteString(m.excludeInput.View() + "\n\n")
	
	// Options
	content.WriteString("Options:\n")
	content.WriteString(fmt.Sprintf("  [%s] Use Regex\n", checkbox(m.useRegex)))
	content.WriteString(fmt.Sprintf("  [%s] Case Sensitive\n\n", checkbox(m.caseSensitive)))
	
	// Log levels
	content.WriteString("Log Levels:\n")
	content.WriteString(fmt.Sprintf("  [%s] ERROR\n", checkbox(m.showError)))
	content.WriteString(fmt.Sprintf("  [%s] WARN\n", checkbox(m.showWarn)))
	content.WriteString(fmt.Sprintf("  [%s] INFO\n", checkbox(m.showInfo)))
	content.WriteString(fmt.Sprintf("  [%s] DEBUG\n", checkbox(m.showDebug)))
	
	style := m.blurredStyle
	if m.focus == LeftPanel {
		style = m.focusedStyle
	}
	
	return style.Width(m.leftWidth).Height(m.height-2).Render(content.String())
}

func (m *VirtualModel) renderRightPanel() string {
	var content strings.Builder
	
	content.WriteString("📜 LOG STREAM\n")
	
	// Position indicator
	position := fmt.Sprintf("(%d-%d/%d)",
		m.viewportStart+1,
		min(m.viewportStart+len(m.visibleEntries), m.totalLines),
		m.totalLines)
	content.WriteString(strings.Repeat(" ", max(0, m.rightWidth-len(position)-15)) + position + "\n")
	
	// Column headers
	headers := "TIME                       LEVEL    MESSAGE\n"
	headers += "───────────────────────────────────────────\n"
	content.WriteString(headers)
	
	// Render visible entries
	m.mutex.RLock()
	for i, entry := range m.visibleEntries {
		isSelected := (i == m.selectedIdx && m.focus == RightPanel)
		line := m.formatLogLine(entry, isSelected)
		content.WriteString(line + "\n")
	}
	m.mutex.RUnlock()
	
	style := m.blurredStyle
	if m.focus == RightPanel {
		style = m.focusedStyle
	}
	
	return style.Width(m.rightWidth).Height(m.height-2).Render(content.String())
}

func (m *VirtualModel) formatLogLine(entry LogEntry, selected bool) string {
	// Extract time portion
	timeStr := entry.Timestamp
	if len(timeStr) > 19 {
		timeStr = timeStr[:19] // Keep only date and time
	}
	
	// Format level with color
	levelStr := fmt.Sprintf("[%s]", entry.Level.String())
	levelStr = m.levelStyles[entry.Level].Render(levelStr)
	
	// Truncate message to fit
	maxMsgLen := m.rightWidth - 40 // Account for time and level
	message := entry.Message
	if len(message) > maxMsgLen {
		message = message[:maxMsgLen-3] + "..."
	}
	
	line := fmt.Sprintf("%-26s %-8s %s", timeStr, levelStr, message)
	
	if selected {
		return "▶ " + m.selectedStyle.Render(line[2:])
	}
	return "  " + line
}

// Scrolling methods
func (m *VirtualModel) scrollDown() {
	m.selectedIdx++
	if m.selectedIdx >= len(m.visibleEntries) {
		m.selectedIdx = len(m.visibleEntries) - 1
		m.viewportStart++
		if m.viewportStart > m.totalLines-m.viewportHeight {
			m.viewportStart = max(0, m.totalLines-m.viewportHeight)
		}
	}
	m.absoluteIdx = m.viewportStart + m.selectedIdx
}

func (m *VirtualModel) scrollUp() {
	m.selectedIdx--
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
		m.viewportStart--
		if m.viewportStart < 0 {
			m.viewportStart = 0
		}
	}
	m.absoluteIdx = m.viewportStart + m.selectedIdx
}

func (m *VirtualModel) scrollHalfPageDown() {
	jump := m.viewportHeight / 2
	m.viewportStart += jump
	if m.viewportStart > m.totalLines-m.viewportHeight {
		m.viewportStart = max(0, m.totalLines-m.viewportHeight)
	}
	m.absoluteIdx = m.viewportStart + m.selectedIdx
}

func (m *VirtualModel) scrollHalfPageUp() {
	jump := m.viewportHeight / 2
	m.viewportStart -= jump
	if m.viewportStart < 0 {
		m.viewportStart = 0
	}
	m.absoluteIdx = m.viewportStart + m.selectedIdx
}

func (m *VirtualModel) scrollToTop() {
	m.viewportStart = 0
	m.selectedIdx = 0
	m.absoluteIdx = 0
}

func (m *VirtualModel) scrollToBottom() {
	m.viewportStart = max(0, m.totalLines-m.viewportHeight)
	m.selectedIdx = min(m.viewportHeight-1, m.totalLines-1)
	m.absoluteIdx = m.totalLines - 1
}

// requestVisibleData requests data for current viewport
func (m *VirtualModel) requestVisibleData() tea.Cmd {
	return func() tea.Msg {
		if m.activeFile != "" && m.reader != nil {
			indexer := m.reader.GetIndexer(m.activeFile)
			if indexer != nil {
				entries, _ := indexer.GetLines(m.viewportStart, m.viewportStart+m.viewportHeight)
				return LogWindowMsg{
					Entries:    entries,
					TotalLines: indexer.GetLineCount(),
					StartLine:  m.viewportStart,
				}
			}
		}
		return nil
	}
}

// updateVisibleFromStream updates visible entries from stream buffer
func (m *VirtualModel) updateVisibleFromStream() {
	if len(m.streamBuffer) == 0 {
		return
	}
	
	// For streaming, show last N entries
	start := max(0, len(m.streamBuffer)-m.viewportHeight)
	end := len(m.streamBuffer)
	
	m.visibleEntries = m.streamBuffer[start:end]
	m.viewportStart = start
	
	if m.tailing {
		m.selectedIdx = len(m.visibleEntries) - 1
	}
}

func (m *VirtualModel) SetReader(reader *StreamReader) {
	m.reader = reader
}

func checkbox(checked bool) string {
	if checked {
		return "✓"
	}
	return " "
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}