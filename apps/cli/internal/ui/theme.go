package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func FormTheme() *huh.Theme {
	theme := huh.ThemeCharm()
	primary := lipgloss.AdaptiveColor{Light: "#5948E8", Dark: "#8B80FF"}
	accent := lipgloss.AdaptiveColor{Light: "#008CBF", Dark: "#43B8F5"}
	success := lipgloss.AdaptiveColor{Light: "#008A63", Dark: "#30C48D"}

	theme.Focused.Title = theme.Focused.Title.Foreground(primary)
	theme.Focused.Base = theme.Focused.Base.BorderForeground(primary)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(accent)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(accent)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(success)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Background(primary)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(success)
	theme.Group.Title = theme.Focused.Title
	return theme
}
