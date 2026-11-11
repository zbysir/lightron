package pack

import (
	"strings"
	"text/template"
)

// NSISScript is the Windows installer (makensis): per user, no admin rights, into %LOCALAPPDATA%\Programs\<Name>;
// Start menu and desktop shortcuts; an entry in Installed apps; it starts the app when done. An update runs it with /S
// (update.Install): it installs over the installed one, then starts it, showing its progress window all the same.
// It waits up to five seconds for a running app to quit before killing it (an older updater quits after starting it).
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

Function .onInit
  ; An update runs this with /S. Show the progress window anyway: the app has just closed, and ten seconds with
  ; nothing on screen look like a crash (people then start the app again in the middle of the install).
  IfSilent 0 +2
    SetSilent normal
FunctionEnd

Section
  ; An updater that starts this installer and then quits (an older app's: it doesn't wait) may still be up for a
  ; moment, and this installer is its child: killing its tree now would kill the installer too. Give it a few
  ; seconds to go on its own first.
  StrCpy $1 0
  wait_quit:
    nsExec::Exec 'cmd /c tasklist /FI "IMAGENAME eq {{.Exe}}" /NH | find /I "{{.Exe}}"'
    Pop $0
    StrCmp $0 "0" 0 quit_done
    IntOp $1 $1 + 1
    IntCmp $1 20 quit_done
    Sleep 250
    Goto wait_quit
  quit_done:
  ; the app may be running (a manual reinstall): its files can't be replaced while in use.
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
