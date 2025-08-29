package main

import (
	"fmt"
	"strings"
	"sync"

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

	// UI state
	focus        PanelFocus
	width        int
	height       int
	selectedIdx  int
	scrollOffset int
	viewMode     ViewMode
	leftWidth    int // Fixed left panel width
	rightWidth   int // Fixed right panel width

	// Filter inputs
	includeInput textinput.Model
	excludeInput textinput.Model
	activeInput  *textinput.Model

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
	)
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

		// Handle input mode
		if m.activeInput != nil {
			switch msg.String() {
			case "esc":
				m.activeInput.Blur()
				m.activeInput = nil
				m.focus = RightPanel
				return m, nil
			case "enter":
				m.activeInput.Blur()
				m.activeInput = nil
				m.focus = RightPanel
				m.applyFilters()
				return m, nil
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
			}
			return m, nil

		case "i":
			m.focus = LeftPanel
			m.includeInput.Focus()
			m.activeInput = &m.includeInput
			return m, nil

		case "e":
			m.focus = LeftPanel
			m.excludeInput.Focus()
			m.activeInput = &m.excludeInput
			return m, nil

		case "/":
			m.focus = LeftPanel
			m.includeInput.Focus()
			m.activeInput = &m.includeInput
			return m, nil

		case "\\":
			m.focus = LeftPanel
			m.excludeInput.Focus()
			m.activeInput = &m.excludeInput
			return m, nil

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
	}

	// Update active input
	if m.activeInput != nil {
		var cmd tea.Cmd
		*m.activeInput, cmd = m.activeInput.Update(msg)
		cmds = append(cmds, cmd)

		// Apply filters when input changes (but not on every keystroke for performance)
		// We'll apply on enter/esc instead
	} else if m.focus == LeftPanel {
		// Update both inputs when not actively editing but in left panel
		var cmd tea.Cmd
		m.includeInput, cmd = m.includeInput.Update(msg)
		cmds = append(cmds, cmd)

		m.excludeInput, cmd = m.excludeInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateLeftPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1", "2", "3", "4":
		// Toggle log levels
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
	case "down", "j":
		if m.selectedIdx < len(m.filteredEntries)-1 {
			m.selectedIdx++
			m.adjustScrollOffset()
		}
	case "home":
		m.selectedIdx = 0
		m.scrollOffset = 0
	case "end":
		m.selectedIdx = len(m.filteredEntries) - 1
		m.adjustScrollOffset()
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

	// Include pattern
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(width - 8).
		MaxWidth(width - 8)

	content += labelStyle.Render("Include Pattern:") + "\n"
	includeValue := m.includeInput.Value()
	if includeValue == "" {
		includeValue = "Type to filter..."
	}
	if m.activeInput == &m.includeInput {
		content += m.includeInput.View() + "\n"
	} else {
		content += inputStyle.Render(includeValue) + "\n"
	}
	content += "\n"

	// Exclude pattern
	content += labelStyle.Render("Exclude Pattern:") + "\n"
	excludeValue := m.excludeInput.Value()
	if excludeValue == "" {
		excludeValue = "Type to exclude..."
	}
	if m.activeInput == &m.excludeInput {
		content += m.excludeInput.View() + "\n"
	} else {
		content += inputStyle.Render(excludeValue) + "\n"
	}
	content += "\n"

	// Log levels
	content += labelStyle.Render("Log Levels:") + "\n"

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	debugStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	checkbox := func(checked bool) string {
		if checked {
			return "[✓]"
		}
		return "[ ]"
	}

	content += fmt.Sprintf("  %s %s  %s %s\n",
		checkbox(m.showError), errorStyle.Render("ERROR"),
		checkbox(m.showWarn), warnStyle.Render("WARN"))
	content += fmt.Sprintf("  %s %s   %s %s\n",
		checkbox(m.showInfo), infoStyle.Render("INFO"),
		checkbox(m.showDebug), debugStyle.Render("DEBUG"))
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
		// List view
		for i := m.scrollOffset; i < m.scrollOffset+availableHeight && i < len(m.filteredEntries); i++ {
			if i < 0 || i >= len(m.filteredEntries) {
				continue
			}

			entry := m.filteredEntries[i]
			line := m.formatFancyLogEntry(entry, width-4)

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

	shortcuts := "j/k=Navigate | Enter=Details | i=Include | e=Exclude | Tab=Switch | q=Quit"
	if m.viewMode == DetailView {
		shortcuts = "ESC=Back to list | q=Quit"
	}

	// Pad content to fill height
	lines := strings.Split(content.String(), "\n")
	for len(lines) < m.height-5 {
		lines = append(lines, "")
	}
	lines = append(lines, footerStyle.Render(shortcuts))

	return panelStyle.Render(strings.Join(lines, "\n"))
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
}

func (m *Model) applyFilters() {
	m.filteredEntries = []LogEntry{}

	includePatterns := strings.Split(m.includeInput.Value(), ",")
	excludePatterns := strings.Split(m.excludeInput.Value(), ",")

	// Clean up patterns
	for i, pattern := range includePatterns {
		includePatterns[i] = strings.TrimSpace(pattern)
	}
	for i, pattern := range excludePatterns {
		excludePatterns[i] = strings.TrimSpace(pattern)
	}

	for _, entry := range m.entries {
		// Check log level filter
		if !m.shouldShowLevel(entry.Level) {
			continue
		}

		// Check include patterns
		if len(includePatterns) > 0 && includePatterns[0] != "" {
			included := false
			for _, pattern := range includePatterns {
				if pattern != "" && strings.Contains(strings.ToLower(entry.Message), strings.ToLower(pattern)) {
					included = true
					break
				}
			}
			if !included {
				continue
			}
		}

		// Check exclude patterns
		if len(excludePatterns) > 0 && excludePatterns[0] != "" {
			excluded := false
			for _, pattern := range excludePatterns {
				if pattern != "" && strings.Contains(strings.ToLower(entry.Message), strings.ToLower(pattern)) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		m.filteredEntries = append(m.filteredEntries, entry)
	}

	// Adjust selection if needed
	if m.selectedIdx >= len(m.filteredEntries) {
		m.selectedIdx = len(m.filteredEntries) - 1
	}
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
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

