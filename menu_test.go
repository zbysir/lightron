package lightron

import (
	"reflect"
	"testing"
)

func TestAccelerator(t *testing.T) {
	for acc, want := range map[string]struct {
		key  string
		mods []string
	}{
		"Cmd+Shift+K": {"k", []string{"cmd", "shift"}},
		"Cmd+,":       {",", []string{"cmd"}},
		"Cmd++":       {"+", []string{"cmd"}},
		"Ctrl+Enter":  {"\r", []string{"ctrl"}},
		"F5":          {"", nil},
		"Alt+Up":      {"", []string{"alt"}},
		"":            {"", nil},
	} {
		k, m := Accelerator(acc)
		if k != want.key || !reflect.DeepEqual(m, want.mods) {
			t.Errorf("Accelerator(%q) = %q %v, want %q %v", acc, k, m, want.key, want.mods)
		}
	}
}

func TestDefaultMenu(t *testing.T) {
	m := DefaultMenu("Notes")
	if m[0].Label != "Notes" || m[1].Role != "editMenu" || m[len(m)-1].Role != "windowMenu" {
		t.Fatalf("default menu: %+v", m)
	}
}
