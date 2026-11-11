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

// An update's installer shows its progress and lets the app that started it quit before killing what is left.
func TestNSISUpdateWaitsAndShows(t *testing.T) {
	s, err := NSISScript(NSIS{Name: "Notes", Exe: "notes.exe"})
	if err != nil {
		t.Fatal(err)
	}
	wait := strings.Index(s, `tasklist /FI "IMAGENAME eq notes.exe"`)
	kill := strings.Index(s, `taskkill /F /T /IM "notes.exe"`)
	if wait < 0 || kill < 0 || wait > kill {
		t.Errorf("want the wait for notes.exe before the taskkill (wait %d, kill %d)", wait, kill)
	}
	if !strings.Contains(s, "SetSilent normal") {
		t.Error("missing SetSilent normal")
	}
}

func TestNSISUninstallStopsApp(t *testing.T) {
	s, err := NSISScript(NSIS{Name: "Notes", Exe: "notes.exe"})
	if err != nil {
		t.Fatal(err)
	}
	_, un, _ := strings.Cut(s, `Section "Uninstall"`)
	kill := strings.Index(un, `taskkill /F /T /IM "notes.exe"`)
	if kill < 0 || kill > strings.Index(un, `RMDir /r "$INSTDIR"`) {
		t.Errorf("the uninstaller should stop the app before deleting its files:\n%s", un)
	}
}
