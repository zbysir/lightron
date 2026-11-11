package pack

import (
	"strings"
	"text/template"
)

// NSISScript is the Windows installer (makensis): per user, no admin rights, into %LOCALAPPDATA%\Programs\<Name>;
// Start menu and desktop shortcuts; an entry in Installed apps; it starts the app when done. An update runs it with /S
// (update.Install): it installs over the installed one, then starts it.
func NSISScript(d NSIS) (string, error) {
	t, err := template.New("nsi").Funcs(template.FuncMap{"winpath": func(p string) string { return strings.ReplaceAll(p, "/", "\\") }}).Parse(nsisTemplate)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	err = t.Execute(&b, d)
	return b.String(), err
}

// NSISDir is a directory to install: Src's contents into $INSTDIR\Dest (Dest with forward slashes).
type NSISDir struct{ Dest, Src string }

// NSIS is what the installer script needs.
type NSIS struct {
	Name, Version, Publisher string
	Exe                      string   // the app's .exe, installed as is
	Files                    []string // absolute paths of the files to install next to the exe (the exe included)
	Dirs                     []NSISDir
	Include                  string   // an NSIS script to !include; its macro preInstall runs before the files are copied
	Out                      string   // the installer's path
	Icon                     string   // a bitmap .ico for the installer itself
	ShortcutIcon             string   // the .ico installed for shortcuts (one of Files' names), "" = the exe's
	RegKey                   string   // the uninstall registry key's name
	Protocols                []string // URL schemes: registered for the current user (the app also does at each start)
}

const nsisTemplate = `Unicode true
{{if .Include}}!include "{{.Include}}"
{{end}}
SetCompressor /SOLID lzma
RequestExecutionLevel user
!define KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\{{.RegKey}}"
Name "{{.Name}}"
OutFile "{{.Out}}"
{{if .Icon}}Icon "{{.Icon}}"
UninstallIcon "{{.Icon}}"
{{end}}InstallDir "$LOCALAPPDATA\Programs\{{.Name}}"
InstallDirRegKey HKCU "${KEY}" "InstallLocation"
ShowInstDetails nevershow
AutoCloseWindow true
Page instfiles

Section
  ; the app may be running (a manual reinstall; an update quits it first): its files can't be replaced while in use.
  ; What it started (a backend on a pipe) goes with it.
  nsExec::Exec 'taskkill /F /T /IM "{{.Exe}}"'
  Sleep 1000
!ifmacrodef preInstall
  !insertmacro preInstall
!endif
  SetOutPath "$INSTDIR"
{{range .Files}}  File "{{.}}"
{{end}}{{range .Dirs}}  RMDir /r "$INSTDIR\{{winpath .Dest}}"
  SetOutPath "$INSTDIR\{{winpath .Dest}}"
  File /r "{{.Src}}"
{{end}}  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortCut "$SMPROGRAMS\{{.Name}}.lnk" "$INSTDIR\{{.Exe}}"{{if .ShortcutIcon}} "" "$INSTDIR\{{.ShortcutIcon}}"{{end}}
  CreateShortCut "$DESKTOP\{{.Name}}.lnk" "$INSTDIR\{{.Exe}}"{{if .ShortcutIcon}} "" "$INSTDIR\{{.ShortcutIcon}}"{{end}}
  WriteRegStr HKCU "${KEY}" "DisplayName" "{{.Name}}"
  WriteRegStr HKCU "${KEY}" "DisplayVersion" "{{.Version}}"
  WriteRegStr HKCU "${KEY}" "Publisher" "{{.Publisher}}"
  WriteRegStr HKCU "${KEY}" "DisplayIcon" "$INSTDIR\{{.Exe}}"
  WriteRegStr HKCU "${KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKCU "${KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${KEY}" "NoRepair" 1
{{range .Protocols}}  WriteRegStr HKCU "Software\Classes\{{.}}" "" "URL:{{$.Name}}"
  WriteRegStr HKCU "Software\Classes\{{.}}" "URL Protocol" ""
  WriteRegStr HKCU "Software\Classes\{{.}}\DefaultIcon" "" '"$INSTDIR\{{$.Exe}}",0'
  WriteRegStr HKCU "Software\Classes\{{.}}\shell\open\command" "" '"$INSTDIR\{{$.Exe}}" "%1"'
{{end}}SectionEnd

Function .onInstSuccess
  Exec '"$INSTDIR\{{.Exe}}"'
FunctionEnd

Section "Uninstall"
  RMDir /r "$INSTDIR"
  Delete "$SMPROGRAMS\{{.Name}}.lnk"
  Delete "$DESKTOP\{{.Name}}.lnk"
  DeleteRegKey HKCU "${KEY}"
{{range .Protocols}}  DeleteRegKey HKCU "Software\Classes\{{.}}"
{{end}}SectionEnd
`
