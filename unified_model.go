package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// UnifiedModel combines fast indexing with full feature set
type UnifiedModel struct {
	config  *Config
	parser  *LogParser
	indexer *FastIndexer
	
	// Virtual scrolling state
	visibleEntries  []LogEntry
	totalLines      int
	viewportStart   int
	viewportHeight  int
	
	// Testing support
	entries         []LogEntry     // All entries (for testing)
	filteredEntries []LogEntry     // Filtered entries (for testing)
	
	// UI state
	focus           PanelFocus
	width           int
	height          int
	selectedIdx     int
	scrollOffset    int
	viewMode        ViewMode
	leftWidth       int
	rightWidth      int
	tailing         bool
	lastGPress      int64
	
	// Filter inputs
	includeInput    textinput.Model
	excludeInput    textinput.Model
	activeInput     *textinput.Model
	useRegex        bool
	caseSensitive   bool
	
	// Left panel navigation
	leftPanelItem   int
	editMode        bool
	
	// Log level filters
	showDebug       bool
	showInfo        bool
	showWarn        bool
	showError       bool
	
	// Filtered indices for search
	filteredIndices []int
	matchedIndices  []int
	currentMatchIdx int
	
	// Status
	indexing        bool
	indexTime       time.Duration
	loadingFile     string
	
	// Styles
	focusedStyle    lipgloss.Style
	blurredStyle    lipgloss.Style
	selectedStyle   lipgloss.Style
	headerStyle     lipgloss.Style
	levelStyles     map[LogLevel]lipgloss.Style
	
	mutex           sync.RWMutex
}

func NewUnifiedModel(config *Config) *UnifiedModel {
	includeInput := textinput.New()
	includeInput.Placeholder = "Type to filter..."
	includeInput.CharLimit = 256
	if config.Include != "" {
		includeInput.SetValue(config.Include)
	}

	excludeInput := textinput.New()
	excludeInput.Placeholder = "Type to exclude..."
	excludeInput.CharLimit = 256
	if config.Exclude != "" {
		excludeInput.SetValue(config.Exclude)
	}

	m := &UnifiedModel{
		config:         config,
		parser:         NewLogParser(config.Timezone),
		visibleEntries: make([]LogEntry, 0),
		focus:          RightPanel,
		viewMode:       LogStreamView,
		showDebug:      true,
		showInfo:       true,
		showWarn:       true,
		showError:      true,
		includeInput:   includeInput,
		excludeInput:   excludeInput,
		viewportHeight: 40,
		tailing:        true,
		leftWidth:      40,
		rightWidth:     100,
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

func (m *UnifiedModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
		m.tickCmd(),
	)
}

func (m *UnifiedModel) tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return unifiedTickMsg(t)
	})
}

type unifiedTickMsg time.Time

func (m *UnifiedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewportHeight = m.height - 10
		
		// Calculate fixed panel widths
		m.leftWidth = m.width * 30 / 100
		if m.leftWidth < 25 {
			m.leftWidth = 25
		}
		if m.leftWidth > 40 {
			m.leftWidth = 40
		}
		m.rightWidth = m.width - m.leftWidth
		
		// Reload view for new size
		m.loadVisibleLines()
		return m, nil
		
	case unifiedTickMsg:
		// Check if indexing completed
		if m.indexer != nil && !m.indexing && len(m.visibleEntries) == 0 {
			m.loadVisibleLines()
		}
		return m, m.tickCmd()
		
	case tea.KeyMsg:
		// Handle detail view
		if m.viewMode == DetailView {
			switch msg.String() {
			case "esc", "q":
				m.viewMode = LogStreamView
				return m, nil
			case "j", "down":
				m.scrollOffset++
				return m, nil
			case "k", "up":
				if m.scrollOffset > 0 {
					m.scrollOffset--
				}
				return m, nil
			}
			return m, nil
		}

		// Handle edit mode
		if m.editMode && m.activeInput != nil {
			switch msg.String() {
			case "esc":
				m.activeInput.Blur()
				m.activeInput = nil
				m.editMode = false
				return m, nil
			case "enter":
				m.activeInput.Blur()
				m.activeInput = nil
				m.editMode = false
				m.applyFilters()
				return m, nil
			default:
				var cmd tea.Cmd
				*m.activeInput, cmd = m.activeInput.Update(msg)
				m.applyFilters()
				return m, cmd
			}
		}

		// Global shortcuts
		switch msg.String() {
		case "/":
			m.focus = LeftPanel
			m.leftPanelItem = 0
			m.editMode = true
			m.activeInput = &m.includeInput
			m.includeInput.Focus()
			return m, textinput.Blink
			
		case "\\":
			m.focus = LeftPanel
			m.leftPanelItem = 1
			m.editMode = true
			m.activeInput = &m.excludeInput
			m.excludeInput.Focus()
			return m, textinput.Blink
		}

		// Navigation based on focus
		if m.focus == LeftPanel {
			return m.updateLeftPanel(msg)
		} else {
			return m.updateRightPanel(msg)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *UnifiedModel) updateLeftPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		// Global quit - works from any panel
		if m.indexer != nil {
			m.indexer.Close()
		}
		return m, tea.Quit
		
	case "tab":
		m.focus = RightPanel
		return m, nil
		
	case "j", "down":
		if !m.editMode {
			m.leftPanelItem++
			if m.leftPanelItem > 7 {
				m.leftPanelItem = 0
			}
		}
		return m, nil
		
	case "k", "up":
		if !m.editMode {
			m.leftPanelItem--
			if m.leftPanelItem < 0 {
				m.leftPanelItem = 7
			}
		}
		return m, nil
		
	case "i":
		if m.leftPanelItem <= 1 {
			m.editMode = true
			if m.leftPanelItem == 0 {
				m.activeInput = &m.includeInput
				m.includeInput.Focus()
			} else {
				m.activeInput = &m.excludeInput
				m.excludeInput.Focus()
			}
			return m, textinput.Blink
		}
		return m, nil
		
	case " ", "enter":
		switch m.leftPanelItem {
		case 2:
			m.useRegex = !m.useRegex
			m.applyFilters()
		case 3:
			m.caseSensitive = !m.caseSensitive
			m.applyFilters()
		case 4:
			m.showError = !m.showError
			m.applyFilters()
		case 5:
			m.showWarn = !m.showWarn
			m.applyFilters()
		case 6:
			m.showInfo = !m.showInfo
			m.applyFilters()
		case 7:
			m.showDebug = !m.showDebug
			m.applyFilters()
		}
		return m, nil
	}
	
	return m, nil
}

func (m *UnifiedModel) updateRightPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.indexer != nil {
			m.indexer.Close()
		}
		return m, tea.Quit
		
	case "tab":
		m.focus = LeftPanel
		return m, nil
		
	case "enter":
		if m.selectedIdx >= 0 && m.selectedIdx < len(m.visibleEntries) {
			m.viewMode = DetailView
			m.scrollOffset = 0
		}
		return m, nil
		
	case "j", "down":
		m.scrollDown()
		return m, nil
		
	case "k", "up":
		m.scrollUp()
		return m, nil
		
	case "ctrl+d":
		m.scrollHalfPageDown()
		return m, nil
		
	case "ctrl+u":
		m.scrollHalfPageUp()
		return m, nil
		
	case "G":
		m.scrollToBottom()
		return m, nil
		
	case "g":
		now := time.Now().UnixNano()
		if now-m.lastGPress < 500000000 {
			m.scrollToTop()
		}
		m.lastGPress = now
		return m, nil
		
	case "n":
		m.nextMatch()
		return m, nil
		
	case "N":
		m.prevMatch()
		return m, nil
		
	case "t":
		m.tailing = !m.tailing
		if m.tailing {
			m.scrollToBottom()
		}
		return m, nil
	}
	
	return m, nil
}

func (m *UnifiedModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Build header
	header := m.renderHeader()
	
	// Build panels
	leftPanel := m.renderLeftPanel()
	
	// Right panel changes based on view mode
	var rightPanel string
	if m.viewMode == DetailView {
		rightPanel = m.renderDetailPanel()
	} else {
		rightPanel = m.renderRightPanel()
	}
	
	// Join panels
	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Combine everything
	return lipgloss.JoinVertical(lipgloss.Left, header, panels)
}

func (m *UnifiedModel) renderHeader() string {
	title := " Panam Log Viewer "
	
	status := ""
	if m.indexing {
		status = fmt.Sprintf("Indexing %s...", m.loadingFile)
	} else if m.totalLines > 0 {
		status = fmt.Sprintf("Lines: %d/%d", len(m.filteredIndices), m.totalLines)
		if m.indexTime > 0 {
			status += fmt.Sprintf(" | Loaded in %v", m.indexTime)
		}
	}
	
	liveIndicator := ""
	if m.tailing {
		liveIndicator = " | Live ●"
	}
	
	padding := m.width - len(title) - len(status) - len(liveIndicator)
	if padding < 0 {
		padding = 0
	}
	
	headerText := title + strings.Repeat(" ", padding) + status + liveIndicator
	return m.headerStyle.Width(m.width).Render(headerText)
}

func (m *UnifiedModel) renderLeftPanel() string {
	var content strings.Builder
	
	content.WriteString("🔍 SEARCH & FILTERS\n\n")
	
	// Include filter
	if m.leftPanelItem == 0 && m.focus == LeftPanel && !m.editMode {
		content.WriteString("▶ ")
	} else {
		content.WriteString("  ")
	}
	content.WriteString("Include Pattern:\n   ")
	if m.leftPanelItem == 0 && m.editMode {
		content.WriteString(m.includeInput.View())
	} else {
		value := m.includeInput.Value()
		if value == "" {
			value = "Type to filter..."
		}
		content.WriteString(value)
	}
	content.WriteString("\n\n")
	
	// Exclude filter
	if m.leftPanelItem == 1 && m.focus == LeftPanel && !m.editMode {
		content.WriteString("▶ ")
	} else {
		content.WriteString("  ")
	}
	content.WriteString("Exclude Pattern:\n   ")
	if m.leftPanelItem == 1 && m.editMode {
		content.WriteString(m.excludeInput.View())
	} else {
		value := m.excludeInput.Value()
		if value == "" {
			value = "Type to exclude..."
		}
		content.WriteString(value)
	}
	content.WriteString("\n\n")
	
	// Options
	content.WriteString("Options:\n")
	if m.leftPanelItem == 2 && m.focus == LeftPanel {
		content.WriteString("▶ ")
	} else {
		content.WriteString("  ")
	}
	content.WriteString(fmt.Sprintf("[%s] Use Regex\n", checkbox(m.useRegex)))
	
	if m.leftPanelItem == 3 && m.focus == LeftPanel {
		content.WriteString("▶ ")
	} else {
		content.WriteString("  ")
	}
	content.WriteString(fmt.Sprintf("[%s] Case Sensitive\n\n", checkbox(m.caseSensitive)))
	
	// Log levels
	content.WriteString("Log Levels:\n")
	levels := []struct {
		name    string
		enabled bool
		index   int
	}{
		{"ERROR", m.showError, 4},
		{"WARN", m.showWarn, 5},
		{"INFO", m.showInfo, 6},
		{"DEBUG", m.showDebug, 7},
	}
	
	for _, level := range levels {
		if m.leftPanelItem == level.index && m.focus == LeftPanel {
			content.WriteString("▶ ")
		} else {
			content.WriteString("  ")
		}
		content.WriteString(fmt.Sprintf("[%s] %s\n", checkbox(level.enabled), level.name))
	}
	
	// Files section at the bottom
	if m.loadingFile != "" {
		content.WriteString("\n📁 Files:\n")
		content.WriteString(fmt.Sprintf("  • %s\n", m.loadingFile))
	}
	
	style := m.blurredStyle
	if m.focus == LeftPanel {
		style = m.focusedStyle
	}
	
	return style.Width(m.leftWidth).Height(m.height-2).Render(content.String())
}

func (m *UnifiedModel) renderRightPanel() string {
	var content strings.Builder
	
	content.WriteString("📜 LOG STREAM\n")
	
	// Position indicator
	position := ""
	if len(m.matchedIndices) > 0 {
		position = fmt.Sprintf("(%d/%d matches)", m.currentMatchIdx+1, len(m.matchedIndices))
	} else if m.totalLines > 0 {
		position = fmt.Sprintf("(%d/%d)", m.viewportStart+m.selectedIdx+1, len(m.filteredIndices))
	}
	
	if position != "" {
		padding := m.rightWidth - 15 - len(position)
		if padding > 0 {
			content.WriteString(strings.Repeat(" ", padding))
		}
		content.WriteString(position + "\n")
	} else {
		content.WriteString("\n")
	}
	
	// Column headers
	content.WriteString("TIME                       LEVEL    MESSAGE\n")
	content.WriteString("───────────────────────────────────────────\n")
	
	// Render visible entries
	m.mutex.RLock()
	for i, entry := range m.visibleEntries {
		isSelected := i == m.selectedIdx
		isMatch := m.isEntryMatch(m.viewportStart + i)
		line := m.formatColumnLogEntry(entry, isSelected, isMatch)
		content.WriteString(line + "\n")
	}
	m.mutex.RUnlock()
	
	style := m.blurredStyle
	if m.focus == RightPanel {
		style = m.focusedStyle
	}
	
	return style.Width(m.rightWidth).Height(m.height-2).Render(content.String())
}

func (m *UnifiedModel) renderDetailPanel() string {
	var content strings.Builder
	
	content.WriteString("📄 DETAIL VIEW\n")
	content.WriteString("              (Press ESC to return)\n")
	content.WriteString("───────────────────────────────────────────\n")
	
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.visibleEntries) {
		content.WriteString("\nNo entry selected\n")
	} else {
		entry := m.visibleEntries[m.selectedIdx]
		
		// Entry details
		content.WriteString(fmt.Sprintf("\nTimestamp: %s\n", entry.Timestamp))
		content.WriteString(fmt.Sprintf("Level:     %s\n", entry.Level))
		if entry.Source != "" {
			content.WriteString(fmt.Sprintf("Source:    %s\n", entry.Source))
		}
		content.WriteString("\nMessage:\n")
		content.WriteString("────────\n")
		
		// Wrap message
		lines := strings.Split(entry.Message, "\n")
		visibleLines := len(lines) - m.scrollOffset
		maxLines := m.height - 15
		if visibleLines > maxLines {
			visibleLines = maxLines
		}
		
		for i := m.scrollOffset; i < m.scrollOffset+visibleLines && i < len(lines); i++ {
			content.WriteString(lines[i] + "\n")
		}
		
		// Metadata if present
		if len(entry.Metadata) > 0 {
			content.WriteString("\nMetadata:\n")
			content.WriteString("─────────\n")
			for k, v := range entry.Metadata {
				content.WriteString(fmt.Sprintf("%s: %v\n", k, v))
			}
		}
	}
	
	style := m.blurredStyle
	if m.focus == RightPanel {
		style = m.focusedStyle
	}
	
	return style.Width(m.rightWidth).Height(m.height-2).Render(content.String())
}

// Keep old function for compatibility but unused
func (m *UnifiedModel) renderDetailView() string {
	return ""
}

func (m *UnifiedModel) formatColumnLogEntry(entry LogEntry, selected, isMatch bool) string {
	// Time column (26 chars)
	timeStr := entry.Timestamp
	if len(timeStr) > 26 {
		timeStr = timeStr[:26]
	} else if len(timeStr) < 26 {
		timeStr = timeStr + strings.Repeat(" ", 26-len(timeStr))
	}
	
	// Level column (8 chars)
	levelStr := fmt.Sprintf("[%s]", entry.Level.String())
	levelStyled := m.levelStyles[entry.Level].Render(levelStr)
	levelPadding := 8 - len(levelStr)
	if levelPadding > 0 {
		levelStyled += strings.Repeat(" ", levelPadding)
	}
	
	// Message column (remaining width)
	maxMsgLen := m.rightWidth - 40
	if maxMsgLen < 20 {
		maxMsgLen = 20
	}
	
	message := strings.ReplaceAll(entry.Message, "\n", " ")
	message = strings.ReplaceAll(message, "\t", " ")
	
	if isMatch {
		message = m.highlightMatches(message)
	}
	
	if len(message) > maxMsgLen {
		message = message[:maxMsgLen-3] + "..."
	}
	
	// Build line
	line := fmt.Sprintf("%s %s %s", timeStr, levelStyled, message)
	
	if selected {
		return "▶ " + m.selectedStyle.Render(line[2:])
	}
	return "  " + line
}

// Load visible lines from indexer
func (m *UnifiedModel) loadVisibleLines() {
	if m.indexer == nil || m.indexing {
		return
	}
	
	start := m.viewportStart
	end := start + m.viewportHeight
	
	// Apply filters to get filtered indices
	if len(m.filteredIndices) == 0 {
		m.applyFilters()
	}
	
	// Load only filtered entries
	visibleIndices := []int{}
	for i := start; i < end && i < len(m.filteredIndices); i++ {
		visibleIndices = append(visibleIndices, m.filteredIndices[i])
	}
	
	if len(visibleIndices) == 0 {
		m.visibleEntries = []LogEntry{}
		return
	}
	
	// Get entries from indexer
	entries := make([]LogEntry, 0, len(visibleIndices))
	for _, idx := range visibleIndices {
		if entry, err := m.indexer.GetLineRange(idx, idx+1); err == nil && len(entry) > 0 {
			entries = append(entries, entry[0])
		}
	}
	
	m.mutex.Lock()
	m.visibleEntries = entries
	m.mutex.Unlock()
}

// Apply filters and update filtered indices
func (m *UnifiedModel) applyFilters() {
	if m.indexer == nil {
		return
	}
	
	m.filteredIndices = []int{}
	m.matchedIndices = []int{}
	
	includePatterns := strings.Split(m.includeInput.Value(), ",")
	excludePatterns := strings.Split(m.excludeInput.Value(), ",")
	
	// Clean patterns
	for i := range includePatterns {
		includePatterns[i] = strings.TrimSpace(includePatterns[i])
	}
	for i := range excludePatterns {
		excludePatterns[i] = strings.TrimSpace(excludePatterns[i])
	}
	
	// Filter through all lines (this is still fast with indexing)
	for i := 0; i < m.totalLines; i++ {
		// Load entry to check level and patterns
		if entries, err := m.indexer.GetLineRange(i, i+1); err == nil && len(entries) > 0 {
			entry := entries[0]
			
			// Check log level filter
			if !m.shouldShowLevel(entry.Level) {
				continue
			}
			
			// Check exclude patterns
			excluded := false
			for _, pattern := range excludePatterns {
				if pattern != "" && m.matchesPattern(entry.Message, pattern) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			
			// Check include patterns
			if m.includeInput.Value() != "" {
				matched := false
				for _, pattern := range includePatterns {
					if pattern != "" && m.matchesPattern(entry.Message, pattern) {
						matched = true
						m.matchedIndices = append(m.matchedIndices, len(m.filteredIndices))
						break
					}
				}
				if !matched {
					continue
				}
			}
			
			m.filteredIndices = append(m.filteredIndices, i)
		}
	}
	
	// Reset viewport if needed
	if m.viewportStart >= len(m.filteredIndices) {
		m.viewportStart = 0
		m.selectedIdx = 0
	}
	
	// Reload visible lines
	m.loadVisibleLines()
}

func (m *UnifiedModel) shouldShowIndex(idx int) bool {
	// For now, always return true since we'd need to load the entry to check level
	// This could be optimized by storing level in the index
	return true
}

func (m *UnifiedModel) shouldShowLevel(level LogLevel) bool {
	switch level {
	case ERROR:
		return m.showError
	case WARN:
		return m.showWarn
	case INFO:
		return m.showInfo
	case DEBUG:
		return m.showDebug
	default:
		return true
	}
}

func (m *UnifiedModel) matchesPattern(text, pattern string) bool {
	if m.useRegex {
		if m.caseSensitive {
			if re, err := regexp.Compile(pattern); err == nil {
				return re.MatchString(text)
			}
		} else {
			if re, err := regexp.Compile("(?i)" + pattern); err == nil {
				return re.MatchString(text)
			}
		}
	} else {
		if m.caseSensitive {
			return strings.Contains(text, pattern)
		} else {
			return strings.Contains(strings.ToLower(text), strings.ToLower(pattern))
		}
	}
	return false
}

func (m *UnifiedModel) highlightMatches(message string) string {
	if m.includeInput.Value() == "" {
		return message
	}
	
	// Simple highlighting with color
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)
	
	patterns := strings.Split(m.includeInput.Value(), ",")
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		
		if m.useRegex {
			// For regex, just highlight the first match
			var re *regexp.Regexp
			if m.caseSensitive {
				re, _ = regexp.Compile(pattern)
			} else {
				re, _ = regexp.Compile("(?i)" + pattern)
			}
			if re != nil {
				loc := re.FindStringIndex(message)
				if loc != nil {
					before := message[:loc[0]]
					match := message[loc[0]:loc[1]]
					after := message[loc[1]:]
					return before + highlightStyle.Render(match) + after
				}
			}
		} else {
			// Simple string highlighting
			var idx int
			if m.caseSensitive {
				idx = strings.Index(message, pattern)
			} else {
				idx = strings.Index(strings.ToLower(message), strings.ToLower(pattern))
			}
			if idx >= 0 {
				before := message[:idx]
				match := message[idx : idx+len(pattern)]
				after := message[idx+len(pattern):]
				return before + highlightStyle.Render(match) + after
			}
		}
	}
	
	return message
}

func (m *UnifiedModel) isEntryMatch(idx int) bool {
	for _, matchIdx := range m.matchedIndices {
		if matchIdx == idx {
			return true
		}
	}
	return false
}

// Scrolling methods
func (m *UnifiedModel) scrollDown() {
	m.selectedIdx++
	if m.selectedIdx >= m.viewportHeight || m.selectedIdx >= len(m.visibleEntries) {
		m.selectedIdx = min(m.viewportHeight-1, len(m.visibleEntries)-1)
		m.viewportStart++
		if m.viewportStart+m.viewportHeight > len(m.filteredIndices) {
			m.viewportStart = max(0, len(m.filteredIndices)-m.viewportHeight)
		}
		m.loadVisibleLines()
	}
}

func (m *UnifiedModel) scrollUp() {
	m.selectedIdx--
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
		m.viewportStart--
		if m.viewportStart < 0 {
			m.viewportStart = 0
		}
		m.loadVisibleLines()
	}
}

func (m *UnifiedModel) scrollHalfPageDown() {
	if len(m.filteredIndices) == 0 {
		return
	}
	
	jump := max(1, m.viewportHeight / 2)
	oldStart := m.viewportStart
	maxStart := max(0, len(m.filteredIndices)-m.viewportHeight)
	
	// Calculate new position
	newStart := m.viewportStart + jump
	
	// If we would go past the end, go to the absolute bottom
	if newStart > maxStart {
		newStart = maxStart
	}
	
	// If we're at the bottom and trying to scroll further, try to show last entry
	if m.viewportStart == maxStart && len(m.filteredIndices) > m.viewportHeight {
		// Move selection to the last visible entry
		m.selectedIdx = min(m.viewportHeight-1, len(m.filteredIndices)-m.viewportStart-1)
		return
	}
	
	m.viewportStart = newStart
	
	// Adjust selection to stay reasonable
	maxVisibleIdx := min(m.viewportHeight-1, len(m.filteredIndices)-m.viewportStart-1)
	if m.selectedIdx > maxVisibleIdx {
		m.selectedIdx = max(0, maxVisibleIdx)
	}
	
	// Only reload if position actually changed
	if m.viewportStart != oldStart {
		m.loadVisibleLines()
	}
}

func (m *UnifiedModel) scrollHalfPageUp() {
	if len(m.filteredIndices) == 0 {
		return
	}
	
	jump := max(1, m.viewportHeight / 2)
	oldStart := m.viewportStart
	
	// Calculate new position
	newStart := m.viewportStart - jump
	
	// If we would go before the start, go to absolute top
	if newStart < 0 {
		newStart = 0
	}
	
	// If we're at the top and trying to scroll further, move selection to first entry
	if m.viewportStart == 0 {
		m.selectedIdx = 0
		return
	}
	
	m.viewportStart = newStart
	
	// Adjust selection to stay reasonable
	maxVisibleIdx := min(m.viewportHeight-1, len(m.filteredIndices)-m.viewportStart-1)
	if m.selectedIdx > maxVisibleIdx {
		m.selectedIdx = max(0, maxVisibleIdx)
	}
	
	// Only reload if position actually changed
	if m.viewportStart != oldStart {
		m.loadVisibleLines()
	}
}

func (m *UnifiedModel) scrollToTop() {
	m.viewportStart = 0
	m.selectedIdx = 0
	m.loadVisibleLines()
}

func (m *UnifiedModel) scrollToBottom() {
	if len(m.filteredIndices) > 0 {
		m.viewportStart = max(0, len(m.filteredIndices)-m.viewportHeight)
		m.selectedIdx = min(m.viewportHeight-1, len(m.filteredIndices)-1)
		m.loadVisibleLines()
	}
}

func (m *UnifiedModel) nextMatch() {
	if len(m.matchedIndices) == 0 {
		return
	}
	
	m.currentMatchIdx++
	if m.currentMatchIdx >= len(m.matchedIndices) {
		m.currentMatchIdx = 0
	}
	
	// Jump to match
	matchPos := m.matchedIndices[m.currentMatchIdx]
	m.viewportStart = max(0, matchPos-m.viewportHeight/2)
	m.selectedIdx = matchPos - m.viewportStart
	m.loadVisibleLines()
}

func (m *UnifiedModel) prevMatch() {
	if len(m.matchedIndices) == 0 {
		return
	}
	
	m.currentMatchIdx--
	if m.currentMatchIdx < 0 {
		m.currentMatchIdx = len(m.matchedIndices) - 1
	}
	
	// Jump to match
	matchPos := m.matchedIndices[m.currentMatchIdx]
	m.viewportStart = max(0, matchPos-m.viewportHeight/2)
	m.selectedIdx = matchPos - m.viewportStart
	m.loadVisibleLines()
}

// Set indexer after file is loaded
func (m *UnifiedModel) SetIndexer(indexer *FastIndexer, filename string) {
	m.indexer = indexer
	m.loadingFile = filename
	m.totalLines = indexer.GetLineCount()
	m.indexing = false
	
	// Initial filter apply
	m.applyFilters()
}

// AddLogEntry adds a log entry to the model (for testing)
func (m *UnifiedModel) AddLogEntry(entry LogEntry) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// If streaming mode, add to entries
	if m.entries == nil {
		m.entries = []LogEntry{}
	}
	m.entries = append(m.entries, entry)
	
	// Add to filtered entries if it passes filters
	includePatterns := strings.Split(m.includeInput.Value(), ",")
	excludePatterns := strings.Split(m.excludeInput.Value(), ",")
	
	// Clean patterns
	for i := range includePatterns {
		includePatterns[i] = strings.TrimSpace(includePatterns[i])
	}
	for i := range excludePatterns {
		excludePatterns[i] = strings.TrimSpace(excludePatterns[i])
	}
	
	// Check log level
	if !m.shouldShowLevel(entry.Level) {
		return
	}
	
	// Check exclude patterns
	for _, pattern := range excludePatterns {
		if pattern != "" && m.matchesPattern(entry.Message, pattern) {
			return
		}
	}
	
	// Check include patterns
	if m.includeInput.Value() != "" {
		matched := false
		for _, pattern := range includePatterns {
			if pattern != "" && m.matchesPattern(entry.Message, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return
		}
	}
	
	// Add to filtered entries
	m.filteredEntries = append(m.filteredEntries, entry)
}

// Helper functions
func checkbox(checked bool) string {
	if checked {
		return "✓"
	}
	return " "
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// renderLogHeader renders the column headers for log display
func (m *UnifiedModel) renderLogHeader(width int) string {
	if width <= 0 {
		return ""
	}
	
	// Calculate column widths
	timeWidth := 19  // "2023-12-23 15:30:45"
	levelWidth := 5  // "ERROR"
	messageWidth := width - timeWidth - levelWidth - 4 // borders
	
	if messageWidth <= 0 {
		return ""
	}
	
	header := fmt.Sprintf("%-*s | %-*s | %s", 
		timeWidth, "TIME", 
		levelWidth, "LEVEL", 
		"MESSAGE")
	
	return header
}

// formatLogEntryColumns formats a log entry into columns
func (m *UnifiedModel) formatLogEntryColumns(entry LogEntry, width int) string {
	if width <= 0 {
		return ""
	}
	
	// Calculate column widths
	timeWidth := 19  // "2023-12-23 15:30:45"
	levelWidth := 5  // "ERROR"
	messageWidth := width - timeWidth - levelWidth - 4 // borders
	
	if messageWidth <= 0 {
		return entry.Message[:min(len(entry.Message), width)]
	}
	
	// Truncate message if too long
	message := entry.Message
	if len(message) > messageWidth {
		message = message[:messageWidth-3] + "..."
	}
	
	formatted := fmt.Sprintf("%-*s | %-*s | %s",
		timeWidth, entry.Timestamp,
		levelWidth, entry.Level.String(),
		message)
	
	return formatted
}