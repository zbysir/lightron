package lightron

import (
	"fmt"
	"strings"
)

// DefaultMenu is the menu bar an app gets when Options.Menu is nil: the app menu (Hide, Hide Others, Show All, Quit),
// Edit and Window. Start from it to add your own:
//
//	menu := lightron.DefaultMenu("Notes")
//	menu = append(menu[:2], append([]lightron.MenuItem{{Label: "File", Submenu: …}}, menu[2:]...)...)
func DefaultMenu(name string) []MenuItem {
	return []MenuItem{
		{Label: name, Submenu: []MenuItem{
			{Role: "hide", Label: "Hide " + name},
			{Role: "hideOthers"},
			{Role: "unhide"},
			Separator,
			{Role: "quit", Label: "Quit " + name},
		}},
		{Role: "editMenu"},
		{Label: "View", Submenu: []MenuItem{
			{Role: "reload"},
			{Role: "toggleFullScreen"},
			Separator,
			{Role: "zoomIn"},
			{Role: "zoomOut"},
			{Role: "resetZoom"},
		}},
		{Role: "windowMenu"},
	}
}

// Accelerator parses an accelerator ("Cmd+Shift+K", "Ctrl+Enter", "F5") into a key equivalent and its modifiers
// (cmd, ctrl, shift, alt), the way AppKit menus take them. An empty string gives "".
func Accelerator(acc string) (key string, mods []string) {
	if acc == "" {
		return "", nil
	}
	parts := strings.Split(acc, "+")
	key = parts[len(parts)-1]
	if key == "" && len(parts) > 1 { // "Cmd++"
		key = "+"
	}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "cmd", "command", "cmdorctrl", "commandorcontrol", "super", "meta":
			mods = append(mods, "cmd")
		case "ctrl", "control":
			mods = append(mods, "ctrl")
		case "shift":
			mods = append(mods, "shift")
		case "alt", "option":
			mods = append(mods, "alt")
		}
	}
	named := map[string]string{
		"enter": "\r", "return": "\r", "tab": "\t", "space": " ", "backspace": "\b", "delete": "\u007f",
		"esc": "\u001b", "escape": "\u001b", "up": "", "down": "", "left": "", "right": "",
		"plus": "+", "home": "", "end": "", "pageup": "", "pagedown": "",
	}
	lk := strings.ToLower(key)
	if v, ok := named[lk]; ok {
		return v, mods
	}
	if len(lk) >= 2 && lk[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(lk[1:], "%d", &n); err == nil && n >= 1 && n <= 20 {
			return string(rune(0xf704 + n - 1)), mods
		}
	}
	return lk, mods
}
