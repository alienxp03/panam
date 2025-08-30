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

type PanelFocus int

const (
	LeftPanel PanelFocus = iota
	RightPanel
)

type ViewMode int

const (
	NormalView ViewMode = iota
	DetailView
)

type Model struct {
	config          *Config
	buffer          *CircularBuffer
	entries         []LogEntry
	filteredEntries []LogEntry
	matchedIndices  []int // Indices of entries that match include pattern
	currentMatchIdx int   // Current position in matchedIndices

	// UI state
	focus        PanelFocus
	width        int
	height       int
	selectedIdx  int
	scrollOffset int
	viewMode     ViewMode
	leftWidth    int // Fixed left panel width
	rightWidth   int // Fixed right panel width
	tailing      bool // Auto-scroll to bottom for live tail
	lastGPress   int64 // For detecting "gg" key combo
	lastUpdate   time.Time // For rate limiting UI updates

	// Filter inputs
	includeInput textinput.Model
	excludeInput textinput.Model
	activeInput  *textinput.Model
	useRegex     bool // Use regex for pattern matching
	caseSensitive bool // Case-sensitive matching
	
	// Left panel navigation
	leftPanelItem int  // 0=include, 1=exclude, 2-3=checkboxes, 4-7=log levels
	editMode      bool // true when editing text fields

	// Log level filters
	showDebug bool
	showInfo  bool
	showWarn  bool
	showError bool

	// Styles
	focusedStyle  lipgloss.Style
	blurredStyle  lipgloss.Style
	selectedStyle lipgloss.Style
	headerStyle   lipgloss.Style
	levelStyles   map[LogLevel]lipgloss.Style

	mutex sync.RWMutex
}

func NewModel(config *Config) *Model {
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

	m := &Model{
		config:          config,
		buffer:          NewCircularBuffer(config.MaxLines),
		entries:         []LogEntry{},
		filteredEntries: []LogEntry{},
		focus:           RightPanel,
		viewMode:        NormalView,
		showDebug:       true,
		showInfo:        true,
		showWarn:        true,
		showError:       true,
		includeInput:    includeInput,
		excludeInput:    excludeInput,
		useRegex:        false,
		caseSensitive:   false,
		tailing:         false,
		matchedIndices:  []int{},
		currentMatchIdx: -1,

		focusedStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("12")).
			BorderTop(true).BorderBottom(true).BorderLeft(true).BorderRight(true),
		blurredStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			BorderTop(true).BorderBottom(true).BorderLeft(true).BorderRight(true),
		selectedStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("12")).
			Foreground(lipgloss.Color("0")).
			Bold(true),
		headerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true).
			Underline(true),
		levelStyles: map[LogLevel]lipgloss.Style{
			ERROR: lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
			WARN:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true),
			INFO:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
			DEBUG: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		},
	}

	m.applyFilters()
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
		tickCmd(), // Start the tick for regular UI updates
	)
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond * 50, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		
		// Calculate fixed panel widths once on resize
		m.leftWidth = m.width * 30 / 100
		if m.leftWidth < 25 {
			m.leftWidth = 25
		}
		if m.leftWidth > 40 {
			m.leftWidth = 40
		}
		m.rightWidth = m.width - m.leftWidth
		
		return m, nil

	case tea.KeyMsg:
		// Handle detail view
		if m.viewMode == DetailView {
			switch msg.String() {
			case "esc", "q":
				m.viewMode = NormalView
				return m, nil
			}
			return m, nil
		}

		// Handle edit mode for text inputs
		if m.editMode && m.activeInput != nil {
			switch msg.String() {
			case "esc":
				m.activeInput.Blur()
				m.activeInput = nil
				m.editMode = false
				// Stay in left panel but exit edit mode
				return m, nil
			case "enter":
				m.activeInput.Blur()
				m.activeInput = nil
				m.editMode = false
				m.applyFilters() // Apply filters in real-time
				return m, nil
			default:
				// Pass through to text input for typing
				var cmd tea.Cmd
				*m.activeInput, cmd = m.activeInput.Update(msg)
				// Apply filters on each keystroke for real-time update
				m.applyFilters()
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			if m.focus == LeftPanel {
				m.focus = RightPanel
			} else {
				m.focus = LeftPanel
				m.leftPanelItem = 0 // Reset to first item
			}
			return m, nil

		case "alt+1":
			// Switch to left panel
			m.focus = LeftPanel
			m.leftPanelItem = 0 // Reset to first item
			return m, nil

		case "alt+2":
			// Switch to right panel
			m.focus = RightPanel
			return m, nil

		case "/":
			// Global shortcut to focus Include field in edit mode
			m.focus = LeftPanel
			m.leftPanelItem = 0
			m.editMode = true
			m.includeInput.Focus()
			m.activeInput = &m.includeInput
			return m, textinput.Blink

		case "\\":
			// Global shortcut to focus Exclude field in edit mode
			m.focus = LeftPanel
			m.leftPanelItem = 1
			m.editMode = true
			m.excludeInput.Focus()
			m.activeInput = &m.excludeInput
			return m, textinput.Blink

		case "c":
			m.includeInput.SetValue("")
			m.excludeInput.SetValue("")
			m.applyFilters()
			return m, nil

		case "enter":
			if m.focus == RightPanel && len(m.filteredEntries) > 0 {
				m.viewMode = DetailView
			}
			return m, nil
		}

		// Handle panel-specific key events
		if m.focus == LeftPanel {
			return m.updateLeftPanel(msg)
		} else {
			return m.updateRightPanel(msg)
		}

	case LogEntryMsg:
		m.AddLogEntry(LogEntry(msg))
		return m, nil
	
	case LogBatchMsg:
		m.AddLogBatch([]LogEntry(msg))
		// Don't trigger immediate redraw for batch, let tick handle it
		return m, nil
	
	case tickMsg:
		// Regular UI update tick
		return m, tickCmd()
	}

	// Don't update inputs here - it's handled in the edit mode section above

	return m, tea.Batch(cmds...)
}

func (m *Model) updateLeftPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.leftPanelItem > 0 {
			m.leftPanelItem--
		}
		return m, nil
		
	case "down", "j":
		if m.leftPanelItem < 7 { // 0-1 for text fields, 2-3 for checkboxes, 4-7 for log levels
			m.leftPanelItem++
		}
		return m, nil
		
	case "i":
		// Enter edit mode for text fields
		if m.leftPanelItem == 0 {
			m.editMode = true
			m.includeInput.Focus()
			m.activeInput = &m.includeInput
			return m, textinput.Blink
		} else if m.leftPanelItem == 1 {
			m.editMode = true
			m.excludeInput.Focus()
			m.activeInput = &m.excludeInput
			return m, textinput.Blink
		}
		return m, nil
		
	case " ", "space":
		// Toggle checkboxes and log levels with space key
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
		
	case "1", "2", "3", "4":
		// Quick toggle with number keys
		switch msg.String() {
		case "1":
			m.showError = !m.showError
		case "2":
			m.showWarn = !m.showWarn
		case "3":
			m.showInfo = !m.showInfo
		case "4":
			m.showDebug = !m.showDebug
		}
		m.applyFilters()
		return m, nil
	}

	return m, nil
}

func (m *Model) updateRightPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selectedIdx > 0 {
			m.selectedIdx--
			m.adjustScrollOffset()
		}
		// Cancel tailing when user navigates up
		m.tailing = false
	case "down", "j":
		if m.selectedIdx < len(m.filteredEntries)-1 {
			m.selectedIdx++
			m.adjustScrollOffset()
		}
	case "ctrl+d":
		// Half page down (vim-like)
		pageSize := m.getLogViewHeight() / 2
		if pageSize < 1 {
			pageSize = 1
		}
		for i := 0; i < pageSize && m.selectedIdx < len(m.filteredEntries)-1; i++ {
			m.selectedIdx++
		}
		m.adjustScrollOffset()
	case "ctrl+u":
		// Half page up (vim-like)
		pageSize := m.getLogViewHeight() / 2
		if pageSize < 1 {
			pageSize = 1
		}
		for i := 0; i < pageSize && m.selectedIdx > 0; i++ {
			m.selectedIdx--
		}
		m.adjustScrollOffset()
	case "home":
		m.selectedIdx = 0
		m.scrollOffset = 0
		m.tailing = false
	case "end":
		m.selectedIdx = len(m.filteredEntries) - 1
		m.adjustScrollOffset()
	case "g":
		// Check for "gg" combo
		now := time.Now().UnixMilli()
		if m.lastGPress > 0 && now-m.lastGPress < 500 {
			// "gg" detected - go to top
			m.selectedIdx = 0
			m.scrollOffset = 0
			m.tailing = false
			m.lastGPress = 0
		} else {
			m.lastGPress = now
		}
	case "G":
		// Go to bottom and enable tailing
		m.selectedIdx = len(m.filteredEntries) - 1
		m.adjustScrollOffset()
		m.tailing = true
	}

	return m, nil
}

func (m *Model) adjustScrollOffset() {
	viewHeight := m.getLogViewHeight()

	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	} else if m.selectedIdx >= m.scrollOffset+viewHeight {
		m.scrollOffset = m.selectedIdx - viewHeight + 1
	}
}

func (m *Model) View() string {
	// Wait for initial size
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Require minimum terminal size
	if m.width < 80 || m.height < 20 {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Padding(2).
			Align(lipgloss.Center)
		return errorStyle.Render(fmt.Sprintf(
			"Terminal too small!\nCurrent: %dx%d\nRequired: 80x20 minimum",
			m.width, m.height))
	}

	// Use pre-calculated panel widths (set in Update when window resizes)
	if m.leftWidth == 0 || m.rightWidth == 0 {
		// Initial calculation if not set
		m.leftWidth = m.width * 30 / 100
		if m.leftWidth < 25 {
			m.leftWidth = 25
		}
		if m.leftWidth > 40 {
			m.leftWidth = 40
		}
		m.rightWidth = m.width - m.leftWidth
	}

	// Render header
	header := m.renderHeader()

	// Render panels with fixed widths
	leftPanel := m.renderFancyLeftPanel(m.leftWidth)
	rightPanel := m.renderFancyRightPanel(m.rightWidth)

	// Join panels horizontally
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Combine header and content
	return lipgloss.JoinVertical(lipgloss.Left, header, mainContent)
}

func (m *Model) renderSimpleLeftPanel(width int) string {
	var lines []string

	// Title
	lines = append(lines, "FILTERS")
	lines = append(lines, strings.Repeat("-", width-2))

	// Include filter
	includeLabel := "Include:"
	includeValue := m.includeInput.Value()
	if m.activeInput == &m.includeInput {
		includeValue = m.includeInput.View()
	}
	lines = append(lines, includeLabel)
	lines = append(lines, includeValue)
	lines = append(lines, "")

	// Exclude filter
	excludeLabel := "Exclude:"
	excludeValue := m.excludeInput.Value()
	if m.activeInput == &m.excludeInput {
		excludeValue = m.excludeInput.View()
	}
	lines = append(lines, excludeLabel)
	lines = append(lines, excludeValue)
	lines = append(lines, "")

	// Log levels
	lines = append(lines, "Levels:")
	if m.showError {
		lines = append(lines, "[✓] ERROR")
	} else {
		lines = append(lines, "[ ] ERROR")
	}
	if m.showWarn {
		lines = append(lines, "[✓] WARN")
	} else {
		lines = append(lines, "[ ] WARN")
	}
	if m.showInfo {
		lines = append(lines, "[✓] INFO")
	} else {
		lines = append(lines, "[ ] INFO")
	}
	if m.showDebug {
		lines = append(lines, "[✓] DEBUG")
	} else {
		lines = append(lines, "[ ] DEBUG")
	}

	// Stats
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Total: %d", len(m.entries)))
	lines = append(lines, fmt.Sprintf("Filtered: %d", len(m.filteredEntries)))

	// Pad to height
	for len(lines) < m.height {
		lines = append(lines, "")
	}

	// Truncate each line to width and pad
	for i, line := range lines {
		if len(line) > width-2 {
			line = line[:width-2]
		}
		lines[i] = line + strings.Repeat(" ", width-len(line)-1) + "│"
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderSimpleRightPanel(width int) string {
	var lines []string

	// Title
	lines = append(lines, "LOG STREAM")
	lines = append(lines, strings.Repeat("-", width-1))

	if m.viewMode == DetailView && m.selectedIdx < len(m.filteredEntries) {
		// Detail view
		entry := m.filteredEntries[m.selectedIdx]
		lines = append(lines, "")
		lines = append(lines, "DETAIL VIEW")
		lines = append(lines, "")
		lines = append(lines, "Time: "+entry.Timestamp)
		lines = append(lines, "Level: "+entry.Level.String())
		lines = append(lines, "Source: "+entry.Source)
		lines = append(lines, "")
		lines = append(lines, "Message:")

		// Word wrap message
		msgLines := m.wrapText(entry.Message, width-2)
		lines = append(lines, msgLines...)

		lines = append(lines, "")
		lines = append(lines, "Press ESC to go back")
	} else {
		// List view
		viewHeight := m.height - 4

		for i := m.scrollOffset; i < m.scrollOffset+viewHeight && i < len(m.filteredEntries); i++ {
			if i < 0 || i >= len(m.filteredEntries) {
				continue
			}

			entry := m.filteredEntries[i]
			line := fmt.Sprintf("%s [%s] %s", entry.Timestamp, entry.Level, entry.Message)

			if len(line) > width-2 {
				line = line[:width-5] + "..."
			}

			if i == m.selectedIdx {
				line = "> " + line
			} else {
				line = "  " + line
			}

			lines = append(lines, line)
		}

		if len(m.filteredEntries) == 0 {
			lines = append(lines, "")
			lines = append(lines, "No log entries to display")
		}
	}

	// Pad to height
	for len(lines) < m.height {
		lines = append(lines, "")
	}

	// Truncate to height
	if len(lines) > m.height {
		lines = lines[:m.height]
	}

	// Ensure each line is exactly the right width
	for i, line := range lines {
		if len(line) > width-1 {
			line = line[:width-1]
		}
		lines[i] = line + strings.Repeat(" ", width-len(line))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) wrapText(text string, width int) []string {
	var lines []string
	words := strings.Fields(text)
	var currentLine string

	for _, word := range words {
		if len(currentLine)+len(word)+1 > width {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		} else {
			if currentLine == "" {
				currentLine = word
			} else {
				currentLine += " " + word
			}
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

func (m *Model) renderHeader() string {
	// Header style
	headerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("238")).
		Foreground(lipgloss.Color("252")).
		Padding(0, 1)

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("117"))
	title := titleStyle.Render("Panam Log Viewer")

	// Status info
	liveIndicator := "●"
	if len(m.entries) > 0 {
		liveIndicator = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("●")
	}

	status := fmt.Sprintf("Lines: %d/%d | Live %s",
		len(m.filteredEntries), m.config.MaxLines, liveIndicator)

	// Calculate padding
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(status) - 4
	if padding < 0 {
		padding = 0
	}

	// Combine
	header := fmt.Sprintf("%s%s%s", title, strings.Repeat(" ", padding), status)

	return headerStyle.Width(m.width).Render(header)
}

func (m *Model) renderFancyLeftPanel(width int) string {
	// Panel styles - ensure exact width
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(width - 2).
		MaxWidth(width - 2).
		Height(m.height - 3).
		MaxHeight(m.height - 3)

	if m.focus == LeftPanel {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("117"))
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("117")).
		MarginBottom(1)

	content := titleStyle.Render("🔍 SEARCH & FILTERS") + "\n"

	// Show loaded files if any
	if len(m.config.Files) > 0 {
		filesStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)
		content += filesStyle.Render("📁 Files:") + "\n"
		for _, file := range m.config.Files {
			// Extract just the filename from the path
			fileName := file
			if strings.Contains(file, "/") {
				parts := strings.Split(file, "/")
				fileName = parts[len(parts)-1]
			}
			fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
			content += "  • " + fileStyle.Render(fileName) + "\n"
		}
		content += "\n"
	}

	// Include pattern
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(width - 8).
		MaxWidth(width - 8)
		

	// Show selection indicator
	includeIndicator := "  "
	if m.focus == LeftPanel && m.leftPanelItem == 0 && !m.editMode {
		includeIndicator = "▶ "
	}
	
	content += includeIndicator + labelStyle.Render("Include Pattern:") + "\n"
	includeValue := m.includeInput.Value()
	if includeValue == "" {
		includeValue = "Type to filter..."
	}
	if m.activeInput == &m.includeInput {
		content += "  " + m.includeInput.View() + "\n"
	} else {
		content += "  " + inputStyle.Render(includeValue) + "\n"
	}
	content += "\n"

	// Exclude pattern
	excludeIndicator := "  "
	if m.focus == LeftPanel && m.leftPanelItem == 1 && !m.editMode {
		excludeIndicator = "▶ "
	}
	
	content += excludeIndicator + labelStyle.Render("Exclude Pattern:") + "\n"
	excludeValue := m.excludeInput.Value()
	if excludeValue == "" {
		excludeValue = "Type to exclude..."
	}
	if m.activeInput == &m.excludeInput {
		content += "  " + m.excludeInput.View() + "\n"
	} else {
		content += "  " + inputStyle.Render(excludeValue) + "\n"
	}
	content += "\n"

	// Pattern matching options
	content += labelStyle.Render("Options:") + "\n"

	regexIndicator := "  "
	caseIndicator := "  "
	
	if m.focus == LeftPanel && !m.editMode {
		switch m.leftPanelItem {
		case 2:
			regexIndicator = "▶ "
		case 3:
			caseIndicator = "▶ "
		}
	}

	checkbox := func(checked bool) string {
		if checked {
			return "[✓]"
		}
		return "[ ]"
	}

	optionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	content += fmt.Sprintf("%s%s %s\n", regexIndicator, checkbox(m.useRegex), optionStyle.Render("Use Regex"))
	content += fmt.Sprintf("%s%s %s\n", caseIndicator, checkbox(m.caseSensitive), optionStyle.Render("Case Sensitive"))
	content += "\n"

	// Log levels
	content += labelStyle.Render("Log Levels:") + "\n"

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	debugStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	// Show indicators for log level selection
	errorIndicator := "  "
	warnIndicator := "  "
	infoIndicator := "  "
	debugIndicator := "  "
	
	if m.focus == LeftPanel && !m.editMode {
		switch m.leftPanelItem {
		case 4:
			errorIndicator = "▶ "
		case 5:
			warnIndicator = "▶ "
		case 6:
			infoIndicator = "▶ "
		case 7:
			debugIndicator = "▶ "
		}
	}
	
	content += fmt.Sprintf("%s%s %s\n", errorIndicator, checkbox(m.showError), errorStyle.Render("ERROR"))
	content += fmt.Sprintf("%s%s %s\n", warnIndicator, checkbox(m.showWarn), warnStyle.Render("WARN"))
	content += fmt.Sprintf("%s%s %s\n", infoIndicator, checkbox(m.showInfo), infoStyle.Render("INFO"))
	content += fmt.Sprintf("%s%s %s\n", debugIndicator, checkbox(m.showDebug), debugStyle.Render("DEBUG"))
	content += "\n"

	// Filter statistics
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		MarginTop(1)

	efficiency := 100.0
	if len(m.entries) > 0 {
		efficiency = float64(len(m.filteredEntries)) / float64(len(m.entries)) * 100
	}

	stats := fmt.Sprintf("📊 Statistics:\n  Total: %d logs\n  Shown: %d logs\n  Efficiency: %.1f%%",
		len(m.entries), len(m.filteredEntries), efficiency)

	content += statsStyle.Render(stats)

	return panelStyle.Render(content)
}

func (m *Model) renderFancyRightPanel(width int) string {
	// Panel styles - ensure exact width
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(width - 2).
		MaxWidth(width - 2).
		Height(m.height - 3).
		MaxHeight(m.height - 3)

	if m.focus == RightPanel {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("117"))
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("117")).
		MarginBottom(1)

	var content strings.Builder
	content.WriteString(titleStyle.Render("📜 LOG STREAM"))

	if len(m.filteredEntries) > 0 {
		content.WriteString(fmt.Sprintf(" (%d/%d)", m.selectedIdx+1, len(m.filteredEntries)))
	}
	content.WriteString("\n")

	// Calculate available height for logs
	availableHeight := m.height - 8 // Account for borders, title, footer

	if m.viewMode == DetailView && m.selectedIdx < len(m.filteredEntries) {
		// Detail view
		content.WriteString(m.renderDetailContent(m.filteredEntries[m.selectedIdx], width-4))
	} else {
		// List view with 3-column design
		
		// Render column headers
		headerStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("245")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("240"))
		
		// Calculate column widths with padding
		timeWidth := 25  // ISO 8601 timestamps are longer
		levelWidth := 7   // [ERROR]
		messageWidth := width - timeWidth - levelWidth - 10 // Account for borders and padding
		
		// Show match indicator in header if there are matches
		matchInfo := ""
		if len(m.matchedIndices) > 0 {
			matchStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("226")).
				Bold(true)
			matchInfo = matchStyle.Render(fmt.Sprintf(" [%d/%d matches]", m.currentMatchIdx+1, len(m.matchedIndices)))
		}
		
		headers := fmt.Sprintf("%-*s  %-*s  %s",
			timeWidth, "TIME",
			levelWidth, "LEVEL",
			"MESSAGE")
		content.WriteString(headerStyle.Render(headers) + matchInfo + "\n")
		
		// Render log entries
		for i := m.scrollOffset; i < m.scrollOffset+availableHeight && i < len(m.filteredEntries); i++ {
			if i < 0 || i >= len(m.filteredEntries) {
				continue
			}

			entry := m.filteredEntries[i]
			// Check if this entry matches the include pattern
			isMatch := m.isEntryMatch(i)
			line := m.formatColumnLogEntry(entry, timeWidth, levelWidth, messageWidth, isMatch)

			if i == m.selectedIdx {
				selectedStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("237")).
					Foreground(lipgloss.Color("117"))
				line = selectedStyle.Render("▶ " + line)
			} else {
				line = "  " + line
			}

			content.WriteString(line + "\n")
		}

		if len(m.filteredEntries) == 0 {
			noDataStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Italic(true)
			content.WriteString("\n" + noDataStyle.Render("No log entries match current filters..."))
		}
	}

	// Footer with shortcuts
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		MarginTop(1)

	shortcuts := "j/k=Nav | n/N=Next/Prev | gg/G=Top/Tail | Alt+1/2=Panel | q=Quit"
	if m.tailing {
		shortcuts += " [TAILING]"
	}
	if len(m.matchedIndices) > 0 {
		matchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
		shortcuts += matchStyle.Render(fmt.Sprintf(" [%d/%d]", m.currentMatchIdx+1, len(m.matchedIndices)))
	}
	if m.viewMode == DetailView {
		shortcuts = "ESC=Back to list | q=Quit"
	}
	if m.focus == LeftPanel {
		if m.editMode {
			shortcuts = "Type to filter | Enter=Apply | ESC=Cancel"
		} else {
			shortcuts = "j/k=Navigate | i=Edit | Space=Toggle | Alt+2=Logs"
		}
	}

	// Pad content to fill height
	lines := strings.Split(content.String(), "\n")
	for len(lines) < m.height-5 {
		lines = append(lines, "")
	}
	lines = append(lines, footerStyle.Render(shortcuts))

	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m *Model) formatColumnLogEntry(entry LogEntry, timeWidth, levelWidth, messageWidth int, isMatch bool) string {
	// Format timestamp (truncate if needed)
	timestamp := entry.Timestamp
	if len(timestamp) > timeWidth {
		timestamp = timestamp[:timeWidth]
	}
	
	// Format level with color
	levelText := fmt.Sprintf("[%s]", entry.Level.String())
	levelStyle := lipgloss.NewStyle()
	switch entry.Level {
	case ERROR:
		levelStyle = levelStyle.Foreground(lipgloss.Color("196"))
	case WARN:
		levelStyle = levelStyle.Foreground(lipgloss.Color("214"))
	case INFO:
		levelStyle = levelStyle.Foreground(lipgloss.Color("117"))
	case DEBUG:
		levelStyle = levelStyle.Foreground(lipgloss.Color("243"))
	}
	coloredLevel := levelStyle.Render(levelText)
	
	// Format message - ensure single line, truncate if needed
	message := strings.ReplaceAll(entry.Message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", "")
	message = strings.TrimSpace(message)
	
	// Highlight matching text if this entry matches
	if isMatch && m.includeInput.Value() != "" {
		message = m.highlightMatches(message)
	}
	
	if messageWidth > 0 && len(message) > messageWidth {
		message = message[:messageWidth-3] + "..."
	}
	
	// Format with proper spacing
	return fmt.Sprintf("%-*s  %s  %s",
		timeWidth, timestamp,
		coloredLevel,
		message)
}

func (m *Model) formatFancyLogEntry(entry LogEntry, width int) string {
	// Level styles
	levelStyle := lipgloss.NewStyle()
	levelText := fmt.Sprintf("[%s]", entry.Level.String())

	switch entry.Level {
	case ERROR:
		levelStyle = levelStyle.Foreground(lipgloss.Color("196"))
	case WARN:
		levelStyle = levelStyle.Foreground(lipgloss.Color("214"))
	case INFO:
		levelStyle = levelStyle.Foreground(lipgloss.Color("117"))
	case DEBUG:
		levelStyle = levelStyle.Foreground(lipgloss.Color("243"))
	}

	// Format timestamp
	timeStr := entry.Timestamp
	if len(timeStr) > 8 {
		timeStr = timeStr[11:19] // Extract HH:MM:SS
	}
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Format source
	source := entry.Source
	if source == "" {
		source = "app"
	}
	if len(source) > 12 {
		source = source[:12]
	}
	sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	// Format message
	message := entry.Message
	maxMsgLen := width - 30 // Account for time, level, source
	if len(message) > maxMsgLen && maxMsgLen > 0 {
		message = message[:maxMsgLen-3] + "..."
	}

	// Add duration if available
	if entry.Metadata != nil {
		if duration, ok := entry.Metadata["duration_ms"]; ok {
			durationStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
			message += " " + durationStyle.Render(fmt.Sprintf("(%vms)", duration))
		}
	}

	return fmt.Sprintf("%s %s %s: %s",
		timeStyle.Render(timeStr),
		levelStyle.Render(levelText),
		sourceStyle.Render(source),
		message)
}

func (m *Model) renderDetailContent(entry LogEntry, width int) string {
	var content strings.Builder

	// Styles
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Bold(true)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	content.WriteString("\n")
	content.WriteString(labelStyle.Render("Timestamp:") + " " + valueStyle.Render(entry.Timestamp) + "\n")
	content.WriteString(labelStyle.Render("Level:") + " " + valueStyle.Render(entry.Level.String()) + "\n")
	content.WriteString(labelStyle.Render("Source:") + " " + valueStyle.Render(entry.Source) + "\n")
	content.WriteString("\n")
	content.WriteString(labelStyle.Render("Message:") + "\n")

	// Wrap message
	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1).
		Width(width - 2)

	content.WriteString(messageStyle.Render(entry.Message))

	// Metadata if available
	if entry.Metadata != nil && len(entry.Metadata) > 0 {
		content.WriteString("\n\n")
		content.WriteString(labelStyle.Render("Metadata:") + "\n")
		for key, value := range entry.Metadata {
			content.WriteString(fmt.Sprintf("  %s: %v\n", key, value))
		}
	}

	return content.String()
}

func (m *Model) renderLeftPanelWithWidth(panelWidth int) string {

	var content strings.Builder

	// Title
	content.WriteString(m.headerStyle.Render("🔍 FILTERS"))
	content.WriteString("\n\n")

	// Filter inputs with labels
	content.WriteString("📥 Include (i): ")
	if m.activeInput == &m.includeInput {
		content.WriteString("🔸 ")
	}
	content.WriteString(m.includeInput.View() + "\n\n")

	content.WriteString("📤 Exclude (e): ")
	if m.activeInput == &m.excludeInput {
		content.WriteString("🔸 ")
	}
	content.WriteString(m.excludeInput.View() + "\n\n")

	// Log level checkboxes
	content.WriteString(m.headerStyle.Render("📊 LOG LEVELS"))
	content.WriteString("\n")
	content.WriteString(m.renderCheckbox("1", "ERROR", m.showError, m.levelStyles[ERROR]))
	content.WriteString(m.renderCheckbox("2", "WARN", m.showWarn, m.levelStyles[WARN]))
	content.WriteString(m.renderCheckbox("3", "INFO", m.showInfo, m.levelStyles[INFO]))
	content.WriteString(m.renderCheckbox("4", "DEBUG", m.showDebug, m.levelStyles[DEBUG]))

	// Statistics
	content.WriteString("\n")
	content.WriteString(m.headerStyle.Render("📈 STATISTICS"))
	content.WriteString("\n")

	// Create a nice stats display
	totalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	filteredStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)

	content.WriteString(fmt.Sprintf("Total:    %s\n", totalStyle.Render(fmt.Sprintf("%d", len(m.entries)))))
	content.WriteString(fmt.Sprintf("Filtered: %s\n", filteredStyle.Render(fmt.Sprintf("%d", len(m.filteredEntries)))))

	// Show filter efficiency
	if len(m.entries) > 0 {
		percentage := float64(len(m.filteredEntries)) / float64(len(m.entries)) * 100
		efficiencyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		content.WriteString(fmt.Sprintf("Showing:  %s\n", efficiencyStyle.Render(fmt.Sprintf("%.1f%%", percentage))))
	}

	// Help text
	content.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	content.WriteString(helpStyle.Render("Hotkeys: i=include, e=exclude, c=clear"))

	style := m.blurredStyle
	if m.focus == LeftPanel {
		style = m.focusedStyle
	}

	// Force exact width (accounting for borders)
	return style.
		Width(panelWidth - 2).
		Height(m.height - 2).
		MaxWidth(panelWidth - 2).
		Render(content.String())
}

func (m *Model) renderCheckbox(key, label string, checked bool, style lipgloss.Style) string {
	checkbox := "☐"
	if checked {
		checkbox = "✅"
	} else {
		checkbox = "❌"
	}

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)

	return fmt.Sprintf("[%s] %s %s\n",
		keyStyle.Render(key),
		checkbox,
		style.Render(label))
}

func (m *Model) renderRightPanelWithWidth(panelWidth int) string {

	if m.viewMode == DetailView {
		return m.renderDetailView(panelWidth)
	}

	var content strings.Builder

	// Header with current selection info
	header := "📜 LOG STREAM"
	if len(m.filteredEntries) > 0 {
		header += fmt.Sprintf(" (%d/%d)", m.selectedIdx+1, len(m.filteredEntries))
	}
	content.WriteString(m.headerStyle.Render(header))
	content.WriteString("\n\n")

	viewHeight := m.getLogViewHeight()

	// Create table-like headers for columns
	if len(m.filteredEntries) > 0 {
		headerWidth := panelWidth - 4
		headerRow := m.renderLogHeader(headerWidth)
		content.WriteString(headerRow + "\n")
		content.WriteString(strings.Repeat("─", headerWidth) + "\n")
	}

	for i := m.scrollOffset; i < m.scrollOffset+viewHeight && i < len(m.filteredEntries); i++ {
		if i < 0 || i >= len(m.filteredEntries) {
			continue
		}

		entry := m.filteredEntries[i]
		entryWidth := panelWidth - 4
		line := m.formatLogEntryColumns(entry, entryWidth)

		if i == m.selectedIdx {
			line = m.selectedStyle.Render(line)
		}

		content.WriteString(line + "\n")
	}

	// Footer with navigation help
	if len(m.filteredEntries) > 0 {
		content.WriteString("\n")
		helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
		content.WriteString(helpStyle.Render("Navigation: ↑↓/jk=scroll, Enter=details, ESC=back"))
	} else {
		content.WriteString("\n")
		noDataStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
		content.WriteString(noDataStyle.Render("No log entries match current filters..."))
	}

	style := m.blurredStyle
	if m.focus == RightPanel {
		style = m.focusedStyle
	}

	// Force exact width (accounting for borders)
	return style.
		Width(panelWidth - 2).
		Height(m.height - 2).
		MaxWidth(panelWidth - 2).
		Render(content.String())
}

func (m *Model) renderLogHeader(width int) string {
	timeWidth := 19 // "2006-01-02 15:04:05"
	levelWidth := 7 // "[ERROR]"
	sourceWidth := 15

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("14")).
		Bold(true).
		Underline(true)

	timeCol := headerStyle.Render(fmt.Sprintf("%-*s", timeWidth, "TIME"))
	levelCol := headerStyle.Render(fmt.Sprintf("%-*s", levelWidth, "LEVEL"))
	sourceCol := headerStyle.Render(fmt.Sprintf("%-*s", sourceWidth, "SOURCE"))
	messageCol := headerStyle.Render("MESSAGE")

	return fmt.Sprintf("%s │ %s │ %s │ %s", timeCol, levelCol, sourceCol, messageCol)
}

func (m *Model) formatLogEntryColumns(entry LogEntry, width int) string {
	// Handle small/negative widths gracefully - just return basic format
	if width < 50 {
		levelStr := fmt.Sprintf("[%s]", entry.Level.String())
		if style, exists := m.levelStyles[entry.Level]; exists {
			levelStr = style.Render(levelStr)
		}
		return fmt.Sprintf("%s %s %s", entry.Timestamp, levelStr, entry.Message)
	}

	timeWidth := 19 // "2006-01-02 15:04:05"
	levelWidth := 7 // "[ERROR]"
	sourceWidth := 15
	messageWidth := width - timeWidth - levelWidth - sourceWidth - 6 // spaces and separators

	// Format timestamp
	timeStr := entry.Timestamp
	if len(timeStr) > timeWidth {
		timeStr = timeStr[:timeWidth]
	}
	timeCol := fmt.Sprintf("%-*s", timeWidth, timeStr)

	// Format level with color
	levelStr := fmt.Sprintf("[%s]", entry.Level.String())
	if style, exists := m.levelStyles[entry.Level]; exists {
		levelStr = style.Render(levelStr)
	}
	levelCol := fmt.Sprintf("%-*s", levelWidth, levelStr)

	// Format source
	sourceStr := entry.Source
	if sourceStr == "" {
		sourceStr = "stdin"
	}
	// Get just filename from path
	if strings.Contains(sourceStr, "/") {
		parts := strings.Split(sourceStr, "/")
		sourceStr = parts[len(parts)-1]
	}
	if len(sourceStr) > sourceWidth {
		sourceStr = sourceStr[:sourceWidth-3] + "..."
	}
	sourceCol := fmt.Sprintf("%-*s", sourceWidth, sourceStr)

	// Format message (handle negative messageWidth)
	message := entry.Message
	if messageWidth > 0 && len(message) > messageWidth {
		message = message[:messageWidth-3] + "..."
	}

	// Add duration if available
	if entry.Metadata != nil {
		if duration, ok := entry.Metadata["duration_ms"]; ok {
			durationStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
			message = fmt.Sprintf("%s %s", message, durationStyle.Render(fmt.Sprintf("(%sms)", duration)))
		}
	}

	return fmt.Sprintf("%s │ %s │ %s │ %s", timeCol, levelCol, sourceCol, message)
}

func (m *Model) renderDetailView(width int) string {
	if len(m.filteredEntries) == 0 || m.selectedIdx >= len(m.filteredEntries) {
		return "No log entry selected"
	}

	entry := m.filteredEntries[m.selectedIdx]

	var content strings.Builder

	// Header
	content.WriteString(m.headerStyle.Render("🔍 LOG ENTRY DETAILS"))
	content.WriteString("\n\n")

	// Entry info with styling
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	content.WriteString(infoStyle.Render("Timestamp: ") + valueStyle.Render(entry.Timestamp) + "\n")

	levelStr := entry.Level.String()
	if style, exists := m.levelStyles[entry.Level]; exists {
		levelStr = style.Render(levelStr)
	}
	content.WriteString(infoStyle.Render("Level:     ") + levelStr + "\n")

	if entry.Source != "" {
		content.WriteString(infoStyle.Render("Source:    ") + valueStyle.Render(entry.Source) + "\n")
	}

	// Duration if available
	if entry.Metadata != nil {
		if duration, ok := entry.Metadata["duration_ms"]; ok {
			durationStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
			content.WriteString(infoStyle.Render("Duration:  ") + durationStyle.Render(fmt.Sprintf("%s ms", duration)) + "\n")
		}
	}

	content.WriteString("\n")
	content.WriteString(infoStyle.Render("Message:"))
	content.WriteString("\n")

	// Message with word wrapping
	messageWidth := width - 6
	messageStyle := lipgloss.NewStyle().
		Width(messageWidth).
		Foreground(lipgloss.Color("15")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	content.WriteString(messageStyle.Render(entry.Message))

	// Metadata section
	if entry.Metadata != nil && len(entry.Metadata) > 0 {
		content.WriteString("\n\n")
		content.WriteString(infoStyle.Render("Metadata:"))
		content.WriteString("\n")

		metadataWidth := width - 6
		metadataStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(1).
			Width(metadataWidth)

		var metadataContent strings.Builder
		for key, value := range entry.Metadata {
			if key != "duration_ms" { // Already shown above
				metadataContent.WriteString(fmt.Sprintf("  %s: %v\n", key, value))
			}
		}

		if metadataContent.Len() > 0 {
			content.WriteString(metadataStyle.Render(metadataContent.String()))
		}
	}

	// Help footer
	content.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	content.WriteString(helpStyle.Render("Press ESC or q to return to log stream"))

	style := m.focusedStyle

	return style.
		Width(width - 2).
		Height(m.height - 2).
		Render(content.String())
}

func (m *Model) formatLogEntry(entry LogEntry) string {
	// Legacy function - now using formatLogEntryColumns
	return m.formatLogEntryColumns(entry, 80)
}

// Backward compatibility functions for tests
func (m *Model) renderLeftPanel() string {
	leftWidth := m.width / 3
	return m.renderLeftPanelWithWidth(leftWidth)
}

func (m *Model) renderRightPanel() string {
	leftWidth := m.width / 3
	rightWidth := m.width - leftWidth
	return m.renderRightPanelWithWidth(rightWidth)
}

func (m *Model) getLogViewHeight() int {
	return m.height - 10 // Account for borders and headers
}

func (m *Model) AddLogEntry(entry LogEntry) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.buffer.Add(entry)
	m.entries = m.buffer.GetAll()
	m.applyFilters()

	// If tailing, auto-scroll to bottom
	if m.tailing && len(m.filteredEntries) > 0 {
		m.selectedIdx = len(m.filteredEntries) - 1
		m.adjustScrollOffset()
	}
}

func (m *Model) AddLogBatch(entries []LogEntry) {
	if len(entries) == 0 {
		return
	}
	
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Add all entries to buffer
	for _, entry := range entries {
		m.buffer.Add(entry)
	}
	
	// Get all entries once after batch add
	m.entries = m.buffer.GetAll()
	
	// Apply filters once for the entire batch
	m.applyFilters()

	// If tailing, auto-scroll to bottom
	if m.tailing && len(m.filteredEntries) > 0 {
		m.selectedIdx = len(m.filteredEntries) - 1
		m.adjustScrollOffset()
	}
}

func (m *Model) applyFilters() {
	m.filteredEntries = []LogEntry{}
	m.matchedIndices = []int{}

	includePatterns := strings.Split(m.includeInput.Value(), ",")
	excludePatterns := strings.Split(m.excludeInput.Value(), ",")

	// Clean up patterns
	for i, pattern := range includePatterns {
		includePatterns[i] = strings.TrimSpace(pattern)
	}
	for i, pattern := range excludePatterns {
		excludePatterns[i] = strings.TrimSpace(pattern)
	}

	// Compile regex patterns if needed
	var includeRegexes []*regexp.Regexp
	var excludeRegexes []*regexp.Regexp
	
	if m.useRegex {
		for _, pattern := range includePatterns {
			if pattern != "" {
				if m.caseSensitive {
					if re, err := regexp.Compile(pattern); err == nil {
						includeRegexes = append(includeRegexes, re)
					}
				} else {
					if re, err := regexp.Compile("(?i)" + pattern); err == nil {
						includeRegexes = append(includeRegexes, re)
					}
				}
			}
		}
		for _, pattern := range excludePatterns {
			if pattern != "" {
				if m.caseSensitive {
					if re, err := regexp.Compile(pattern); err == nil {
						excludeRegexes = append(excludeRegexes, re)
					}
				} else {
					if re, err := regexp.Compile("(?i)" + pattern); err == nil {
						excludeRegexes = append(excludeRegexes, re)
					}
				}
			}
		}
	}

	for _, entry := range m.entries {
		// Check log level filter
		if !m.shouldShowLevel(entry.Level) {
			continue
		}

		// Check exclude patterns first - if excluded, skip this entry
		if len(excludePatterns) > 0 && excludePatterns[0] != "" {
			excluded := false
			if m.useRegex {
				// Use regex matching
				for _, re := range excludeRegexes {
					if re.MatchString(entry.Message) {
						excluded = true
						break
					}
				}
			} else {
				// Use simple string matching
				for _, pattern := range excludePatterns {
					if pattern != "" {
						if m.caseSensitive {
							if strings.Contains(entry.Message, pattern) {
								excluded = true
								break
							}
						} else {
							if strings.Contains(strings.ToLower(entry.Message), strings.ToLower(pattern)) {
								excluded = true
								break
							}
						}
					}
				}
			}
			if excluded {
				continue
			}
		}

		// Add to filtered entries (all non-excluded entries with matching log level)
		m.filteredEntries = append(m.filteredEntries, entry)
		filteredIdx := len(m.filteredEntries) - 1

		// Check include patterns for highlighting/matching
		if len(includePatterns) > 0 && includePatterns[0] != "" {
			matched := false
			if m.useRegex {
				// Use regex matching
				for _, re := range includeRegexes {
					if re.MatchString(entry.Message) {
						matched = true
						break
					}
				}
			} else {
				// Use simple string matching
				for _, pattern := range includePatterns {
					if pattern != "" {
						if m.caseSensitive {
							if strings.Contains(entry.Message, pattern) {
								matched = true
								break
							}
						} else {
							if strings.Contains(strings.ToLower(entry.Message), strings.ToLower(pattern)) {
								matched = true
								break
							}
						}
					}
				}
			}
			if matched {
				m.matchedIndices = append(m.matchedIndices, filteredIdx)
			}
		}
	}

	// Adjust selection if needed
	if m.selectedIdx >= len(m.filteredEntries) {
		m.selectedIdx = len(m.filteredEntries) - 1
	}
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
	}

	// Auto-jump to most recent match if there are matches
	if len(m.matchedIndices) > 0 {
		m.currentMatchIdx = len(m.matchedIndices) - 1 // Most recent match
		m.selectedIdx = m.matchedIndices[m.currentMatchIdx]
		m.adjustScrollOffset()
	} else {
		m.currentMatchIdx = -1
	}

	// If tailing and no matches, scroll to bottom
	if m.tailing && len(m.filteredEntries) > 0 && len(m.matchedIndices) == 0 {
		m.selectedIdx = len(m.filteredEntries) - 1
		m.adjustScrollOffset()
	}
}

func (m *Model) shouldShowLevel(level LogLevel) bool {
	switch level {
	case DEBUG:
		return m.showDebug
	case INFO:
		return m.showInfo
	case WARN:
		return m.showWarn
	case ERROR:
		return m.showError
	default:
		return true
	}
}

type LogEntryMsg LogEntry
type LogBatchMsg []LogEntry

// Helper function to check if an entry at given index matches
func (m *Model) isEntryMatch(idx int) bool {
	for _, matchIdx := range m.matchedIndices {
		if matchIdx == idx {
			return true
		}
	}
	return false
}

// Helper function to highlight matching text in a message
func (m *Model) highlightMatches(message string) string {
	if m.includeInput.Value() == "" {
		return message
	}

	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	includePatterns := strings.Split(m.includeInput.Value(), ",")
	for i, pattern := range includePatterns {
		includePatterns[i] = strings.TrimSpace(pattern)
	}

	// For simplicity, highlight the first matching pattern found
	for _, pattern := range includePatterns {
		if pattern == "" {
			continue
		}

		if m.useRegex {
			var re *regexp.Regexp
			var err error
			if m.caseSensitive {
				re, err = regexp.Compile(pattern)
			} else {
				re, err = regexp.Compile("(?i)" + pattern)
			}
			if err == nil && re != nil {
				matches := re.FindAllStringIndex(message, -1)
				if len(matches) > 0 {
					// Highlight first match for display
					start := matches[0][0]
					end := matches[0][1]
					if start >= 0 && end <= len(message) {
						highlighted := message[:start] + highlightStyle.Render(message[start:end]) + message[end:]
						return highlighted
					}
				}
			}
		} else {
			// Simple string matching
			var idx int
			if m.caseSensitive {
				idx = strings.Index(message, pattern)
			} else {
				idx = strings.Index(strings.ToLower(message), strings.ToLower(pattern))
			}
			if idx >= 0 {
				end := idx + len(pattern)
				if end <= len(message) {
					highlighted := message[:idx] + highlightStyle.Render(message[idx:end]) + message[end:]
					return highlighted
				}
			}
		}
	}

	return message
}

