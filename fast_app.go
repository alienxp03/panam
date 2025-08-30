package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FastApp uses the ultra-fast indexer
type FastApp struct {
	config  *Config
	parser  *LogParser
	indexer *FastIndexer
	
	// UI state
	entries      []LogEntry
	totalLines   int
	viewStart    int
	viewHeight   int
	selectedLine int
	
	// Window size
	width  int
	height int
	
	// Status
	indexing     bool
	indexTime    time.Duration
	
	// Styles
	headerStyle  lipgloss.Style
	levelStyles  map[LogLevel]lipgloss.Style
}

func NewFastApp(config *Config) (*FastApp, error) {
	parser := NewLogParser(config.Timezone)
	
	app := &FastApp{
		config:     config,
		parser:     parser,
		viewHeight: 40,
		entries:    make([]LogEntry, 0),
	}
	
	// Initialize styles
	app.headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	
	app.levelStyles = map[LogLevel]lipgloss.Style{
		DEBUG: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		INFO:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		WARN:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		ERROR: lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}
	
	// Handle different input types
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Piped input - use traditional approach
		return app, nil
	}
	
	// File input - use fast indexer
	if len(config.Files) > 0 {
		indexer, err := NewFastIndexer(config.Files[0], parser)
		if err != nil {
			return nil, err
		}
		app.indexer = indexer
		
		// Start indexing in background
		go app.indexInBackground()
	}
	
	return app, nil
}

func (app *FastApp) indexInBackground() {
	app.indexing = true
	start := time.Now()
	
	if err := app.indexer.IndexFileUltraFast(); err == nil {
		app.indexTime = time.Since(start)
		app.totalLines = app.indexer.GetLineCount()
		app.indexing = false
		
		// Load initial view
		if entries, err := app.indexer.GetLineRange(0, app.viewHeight); err == nil {
			app.entries = entries
		}
	}
}

func (app *FastApp) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
		fastTickCmd(),
	)
}

func fastTickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return fastTickMsg(t)
	})
}

type fastTickMsg time.Time

func (app *FastApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		app.width = msg.Width
		app.height = msg.Height
		app.viewHeight = app.height - 5
		
		// Reload view for new size
		if app.indexer != nil && !app.indexing {
			if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
				app.entries = entries
			}
		}
		return app, nil
		
	case fastTickMsg:
		// Check if indexing completed
		if app.indexer != nil && !app.indexing && len(app.entries) == 0 {
			if entries, err := app.indexer.GetLineRange(0, app.viewHeight); err == nil {
				app.entries = entries
			}
		}
		return app, fastTickCmd()
		
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if app.indexer != nil {
				app.indexer.Close()
			}
			return app, tea.Quit
			
		case "j", "down":
			app.scrollDown()
			return app, nil
			
		case "k", "up":
			app.scrollUp()
			return app, nil
			
		case "ctrl+d":
			app.scrollHalfPageDown()
			return app, nil
			
		case "ctrl+u":
			app.scrollHalfPageUp()
			return app, nil
			
		case "G":
			app.scrollToBottom()
			return app, nil
			
		case "g":
			app.scrollToTop()
			return app, nil
		}
	}
	
	return app, nil
}

func (app *FastApp) View() string {
	if app.width == 0 {
		return "Initializing..."
	}
	
	// Header
	header := app.renderHeader()
	
	// Content
	content := app.renderContent()
	
	return header + "\n" + content
}

func (app *FastApp) renderHeader() string {
	title := " Panam Log Viewer (Fast Mode) "
	
	status := ""
	if app.indexing {
		status = "Indexing..."
	} else if app.indexer != nil {
		status = fmt.Sprintf("Lines: %d | Indexed in %v", app.totalLines, app.indexTime)
	}
	
	padding := app.width - len(title) - len(status)
	if padding < 0 {
		padding = 0
	}
	
	headerText := title + strings.Repeat(" ", padding) + status
	return app.headerStyle.Width(app.width).Render(headerText)
}

func (app *FastApp) renderContent() string {
	if app.indexing {
		return "Indexing file..."
	}
	
	if len(app.entries) == 0 {
		return "No logs to display"
	}
	
	var output strings.Builder
	
	// Column headers
	output.WriteString("TIME                     LEVEL   MESSAGE\n")
	output.WriteString(strings.Repeat("─", app.width) + "\n")
	
	// Render entries
	for i, entry := range app.entries {
		if i >= app.viewHeight-2 {
			break
		}
		
		line := app.formatLine(entry, i == app.selectedLine)
		output.WriteString(line + "\n")
	}
	
	return output.String()
}

func (app *FastApp) formatLine(entry LogEntry, selected bool) string {
	// Format timestamp
	timeStr := entry.Timestamp
	if len(timeStr) > 19 {
		timeStr = timeStr[:19]
	}
	
	// Format level
	levelStr := fmt.Sprintf("[%s]", entry.Level.String())
	levelStr = app.levelStyles[entry.Level].Render(levelStr)
	
	// Truncate message
	maxMsgLen := app.width - 35
	if maxMsgLen < 20 {
		maxMsgLen = 20
	}
	
	message := entry.Message
	if len(message) > maxMsgLen {
		message = message[:maxMsgLen-3] + "..."
	}
	
	prefix := "  "
	if selected {
		prefix = "▶ "
	}
	
	return fmt.Sprintf("%s%-24s %-7s %s", prefix, timeStr, levelStr, message)
}

func (app *FastApp) scrollDown() {
	if app.indexer == nil || app.indexing {
		return
	}
	
	app.selectedLine++
	if app.selectedLine >= app.viewHeight-2 {
		app.selectedLine = app.viewHeight - 3
		app.viewStart++
		
		if app.viewStart+app.viewHeight > app.totalLines {
			app.viewStart = app.totalLines - app.viewHeight
			if app.viewStart < 0 {
				app.viewStart = 0
			}
		}
		
		// Load new data
		if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
			app.entries = entries
		}
	}
}

func (app *FastApp) scrollUp() {
	if app.indexer == nil || app.indexing {
		return
	}
	
	app.selectedLine--
	if app.selectedLine < 0 {
		app.selectedLine = 0
		app.viewStart--
		
		if app.viewStart < 0 {
			app.viewStart = 0
		}
		
		// Load new data
		if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
			app.entries = entries
		}
	}
}

func (app *FastApp) scrollHalfPageDown() {
	if app.indexer == nil || app.indexing {
		return
	}
	
	jump := app.viewHeight / 2
	app.viewStart += jump
	
	if app.viewStart+app.viewHeight > app.totalLines {
		app.viewStart = app.totalLines - app.viewHeight
		if app.viewStart < 0 {
			app.viewStart = 0
		}
	}
	
	// Load new data
	if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
		app.entries = entries
	}
}

func (app *FastApp) scrollHalfPageUp() {
	if app.indexer == nil || app.indexing {
		return
	}
	
	jump := app.viewHeight / 2
	app.viewStart -= jump
	
	if app.viewStart < 0 {
		app.viewStart = 0
	}
	
	// Load new data
	if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
		app.entries = entries
	}
}

func (app *FastApp) scrollToTop() {
	if app.indexer == nil || app.indexing {
		return
	}
	
	app.viewStart = 0
	app.selectedLine = 0
	
	// Load new data
	if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
		app.entries = entries
	}
}

func (app *FastApp) scrollToBottom() {
	if app.indexer == nil || app.indexing {
		return
	}
	
	app.viewStart = app.totalLines - app.viewHeight
	if app.viewStart < 0 {
		app.viewStart = 0
	}
	
	app.selectedLine = app.viewHeight - 3
	if app.selectedLine >= len(app.entries) {
		app.selectedLine = len(app.entries) - 1
	}
	
	// Load new data
	if entries, err := app.indexer.GetLineRange(app.viewStart, app.viewStart+app.viewHeight); err == nil {
		app.entries = entries
	}
}

func RunFastApp(config *Config) error {
	app, err := NewFastApp(config)
	if err != nil {
		return err
	}
	
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run program: %w", err)
	}
	
	return nil
}