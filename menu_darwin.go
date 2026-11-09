package lightron

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// The Go side of the macOS menu library (internal/macmenu): it turns Options.Menu / DockMenu into the spec the library
// puts into the browser's menu bar, and runs what is clicked. The library connects to a unix socket here; lines go
// both ways (the spec and "activate" to it; "click <n>", "action <name>" from it).

type macMenu struct {
	m     *mac
	sock  string
	mu    sync.Mutex
	conns []net.Conn
	spec  []byte
	click map[int]func(*Window)
}

func newMacMenu(m *mac, sock string) (*macMenu, error) {
	mm := &macMenu{m: m, sock: sock, click: map[int]func(*Window){}}
	mm.spec = mm.build(m.app.opts)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mm.mu.Lock()
			mm.conns = append(mm.conns, c)
			spec := mm.spec
			mm.mu.Unlock()
			_, _ = c.Write(append(spec, '\n'))
			go mm.read(c)
		}
	}()
	return mm, nil
}

func (mm *macMenu) read(c net.Conn) {
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		kind, arg, _ := strings.Cut(sc.Text(), " ")
		switch kind {
		case "click":
			n, _ := strconv.Atoi(arg)
			if f := mm.click[n]; f != nil {
				go f(mm.m.focusedWindow())
			}
		case "action":
			switch arg {
			case "reopen": // a click on the Dock icon
				go mm.m.app.FocusOrOpen()
			case "reload":
				if w := mm.m.focusedWindow(); w != nil {
					_, _ = mm.m.c.Call(w.session, "Page.reload", nil)
				}
			}
		}
	}
	mm.mu.Lock()
	for i, x := range mm.conns {
		if x == c {
			mm.conns = append(mm.conns[:i], mm.conns[i+1:]...)
			break
		}
	}
	mm.mu.Unlock()
}

// activate asks the library to bring the app forward from inside the browser process.
func (mm *macMenu) activate() {
	mm.mu.Lock()
	conns := append([]net.Conn(nil), mm.conns...)
	mm.mu.Unlock()
	for _, c := range conns {
		_, _ = c.Write([]byte("activate\n"))
	}
}

type specItem map[string]any

// build: the spec (internal/macmenu/objc/menu.m): "dock" is the Dock menu, "app" the app menu's items, "edit" /
// "window" the browser's own menus in that place, the rest are menus of ours.
func (mm *macMenu) build(o Options) []byte {
	menu := o.Menu
	if menu == nil {
		menu = DefaultMenu(o.Name)
	}
	var spec []specItem
	if len(o.DockMenu) > 0 {
		spec = append(spec, specItem{"role": "dock", "items": mm.items(o.DockMenu)})
	}
	for i, top := range menu {
		switch {
		case i == 0:
			spec = append(spec, specItem{"role": "app", "items": mm.items(top.Submenu)})
		case top.Role == "editMenu":
			spec = append(spec, specItem{"role": "edit", "title": orDefault(top.Label, "Edit")})
		case top.Role == "windowMenu":
			spec = append(spec, specItem{"role": "window", "title": orDefault(top.Label, "Window")})
		default:
			spec = append(spec, specItem{"title": top.Label, "items": mm.items(top.Submenu)})
		}
	}
	b, _ := json.Marshal(spec)
	return b
}

func (mm *macMenu) items(in []MenuItem) []specItem {
	var out []specItem
	for _, it := range in {
		if it.Separator {
			out = append(out, specItem{"separator": true})
			continue
		}
		s := specItem{"title": orDefault(it.Label, roleLabels[it.Role])}
		if key, mods := Accelerator(orDefault(it.Accelerator, roleKeys[it.Role])); key != "" {
			s["key"], s["mods"] = key, mods
		}
		switch {
		case len(it.Submenu) > 0:
			s["items"] = mm.items(it.Submenu)
		case it.Click != nil:
			n := len(mm.click) + 1
			mm.click[n] = it.Click
			s["send"] = fmt.Sprintf("click %d", n)
		case roleSelectors[it.Role] != "":
			s["sel"] = roleSelectors[it.Role]
		case roleBrowser[it.Role] != "":
			s["chrome"] = roleBrowser[it.Role]
		case it.Role == "reload":
			s["send"] = "action reload"
		case it.Role != "":
			mm.m.app.logf("menu: unknown role %q", it.Role)
			continue
		}
		out = append(out, s)
	}
	return out
}

func orDefault(s, d string) string {
	if s != "" {
		return s
	}
	return d
}

// Roles: standard Cocoa actions (sent to the first responder), and items whose behaviour is the browser's (copies of
// its own items, found by action or key).
var (
	roleSelectors = map[string]string{
		"hide": "hide:", "hideOthers": "hideOtherApplications:", "unhide": "unhideAllApplications:", "quit": "terminate:",
		"undo": "undo:", "redo": "redo:", "cut": "cut:", "copy": "copy:", "paste": "paste:", "selectAll": "selectAll:",
		"minimize": "performMiniaturize:", "zoom": "performZoom:", "front": "arrangeInFront:", "close": "performClose:",
	}
	roleBrowser = map[string]string{
		"toggleFullScreen": "toggleFullScreen:", "zoomIn": "key", "zoomOut": "key", "resetZoom": "key", "devTools": "key",
	}
	roleKeys = map[string]string{
		"hide": "Cmd+H", "hideOthers": "Cmd+Alt+H", "quit": "Cmd+Q", "undo": "Cmd+Z", "redo": "Cmd+Shift+Z",
		"cut": "Cmd+X", "copy": "Cmd+C", "paste": "Cmd+V", "selectAll": "Cmd+A", "minimize": "Cmd+M",
		"close": "Cmd+W", "zoomIn": "Cmd+=", "zoomOut": "Cmd+-", "resetZoom": "Cmd+0", "devTools": "Cmd+Alt+I",
		"reload": "Cmd+R",
	}
	roleLabels = map[string]string{
		"hide": "Hide", "hideOthers": "Hide Others", "unhide": "Show All", "quit": "Quit", "undo": "Undo",
		"redo": "Redo", "cut": "Cut", "copy": "Copy", "paste": "Paste", "selectAll": "Select All",
		"minimize": "Minimize", "zoom": "Zoom", "front": "Bring All to Front", "close": "Close Window",
		"toggleFullScreen": "Enter Full Screen", "zoomIn": "Zoom In", "zoomOut": "Zoom Out", "resetZoom": "Actual Size",
		"devTools": "Developer Tools", "reload": "Reload",
	}
)
