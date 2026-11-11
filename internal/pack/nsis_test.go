package pack

import (
	"strings"
	"testing"
)

func TestNSISProtocols(t *testing.T) {
	s, err := NSISScript(NSIS{Name: "Notes", Exe: "notes.exe", Protocols: []string{"notes"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`WriteRegStr HKCU "Software\Classes\notes" "URL Protocol" ""`,
		`WriteRegStr HKCU "Software\Classes\notes\shell\open\command" "" '"$INSTDIR\notes.exe" "%1"'`,
		`DeleteRegKey HKCU "Software\Classes\notes"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
}
