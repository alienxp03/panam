package main

import (
	"testing"

	"github.com/charmbracelet/bubbletea"
)

func TestUI_HotkeyNavigation(t *testing.T) {
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewModel(config)
	
	// Test 'i' key should focus include input
	updatedModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m := updatedModel.(*Model)
	
	if m.focus != LeftPanel {
		t.Error("Expected focus to be on LeftPanel after pressing 'i'")
	}
	
	if m.activeInput != &m.includeInput {
		t.Error("Expected activeInput to be includeInput after pressing 'i'")
	}
	
	// Test 'e' key should focus exclude input
	model = NewModel(config)
	updatedModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updatedModel.(*Model)
	
	if m.focus != LeftPanel {
		t.Error("Expected focus to be on LeftPanel after pressing 'e'")
	}
	
	if m.activeInput != &m.excludeInput {
		t.Error("Expected activeInput to be excludeInput after pressing 'e'")
	}
}

func TestUI_ViewModeToggle(t *testing.T) {
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewModel(config)
	
	// Add a test entry
	entry := LogEntry{
		Message:   "Test log entry",
		Level:     INFO,
		Timestamp: "2023-12-23 15:30:45",
	}
	model.AddLogEntry(entry)
	
	// Test Enter key should switch to detail view
	model.focus = RightPanel
	updatedModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updatedModel.(*Model)
	
	if m.viewMode != DetailView {
		t.Error("Expected viewMode to be DetailView after pressing Enter")
	}
	
	// Test ESC key should return to normal view
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updatedModel.(*Model)
	
	if m.viewMode != NormalView {
		t.Error("Expected viewMode to be NormalView after pressing ESC")
	}
}

func TestUI_InputHandling(t *testing.T) {
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewModel(config)
	
	// Activate include input
	updatedModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m := updatedModel.(*Model)
	
	// Test ESC should deactivate input and return focus to right panel
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updatedModel.(*Model)
	
	if m.activeInput != nil {
		t.Error("Expected activeInput to be nil after pressing ESC")
	}
	
	if m.focus != RightPanel {
		t.Error("Expected focus to return to RightPanel after pressing ESC in input mode")
	}
	
	// Test Enter should also deactivate input and apply filters
	model = NewModel(config)
	updatedModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updatedModel.(*Model)
	
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(*Model)
	
	if m.activeInput != nil {
		t.Error("Expected activeInput to be nil after pressing Enter")
	}
	
	if m.focus != RightPanel {
		t.Error("Expected focus to return to RightPanel after pressing Enter in input mode")
	}
}

func TestUI_LogLevelToggle(t *testing.T) {
	config := &Config{
		MaxLines:    100,
		Files:       []string{},
		RefreshRate: 1,
		Include:     "",
		Exclude:     "",
		Timezone:    "UTC",
	}
	
	model := NewModel(config)
	model.focus = LeftPanel
	
	// Test toggling ERROR level (key '1')
	originalShowError := model.showError
	updatedModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m := updatedModel.(*Model)
	
	if m.showError == originalShowError {
		t.Error("Expected showError to toggle after pressing '1'")
	}
	
	// Test toggling WARN level (key '2')
	originalShowWarn := m.showWarn
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updatedModel.(*Model)
	
	if m.showWarn == originalShowWarn {
		t.Error("Expected showWarn to toggle after pressing '2'")
	}
}