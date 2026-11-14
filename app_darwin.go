package lightron

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"

	"github.com/zbysir/lightron/internal/cdp"
	"github.com/zbysir/lightron/internal/macengine"
	"github.com/zbysir/lightron/internal/macmenu"
	"github.com/zbysir/lightron/internal/pack"
)

//go:embed bridge.js
var bridgeJS string

// ErrNoBrowser: no Chromium-family browser on this Mac (Run has told the user, with a link to download Chrome).
var ErrNoBrowser = macengine.ErrNoBrowser

// mac is the macOS engine: a copy of the user's browser that is the app (internal/macengine), driven over CDP.
type mac struct {
	app     *App
	c       *cdp.Conn
	exe     string // the engine's executable: new windows are asked of it (newWindow)
	profile string
	// entry: the pages' entry, at the page host (http://<Host>/…); origin: its origin. Both "" while a splash shows
	// (Options.Splash: URLFunc still running), set once (setEntry). Read with pages()
	entry   string
	origin  string
	scripts map[string]string // a window's session → its page scripts' identifier (replaced by setEntry)
	splash  string            // the splash page's address while URLFunc runs (Options.Splash)
	menu    *macMenu
	ua      string // the User-Agent with Options.UserAgentSuffix ("": none)

	ctxMu   sync.Mutex
	origins map[string]string // "<session> <execution context id>" → the context's origin: who called the binding
	frames  map[string]string // an out-of-process iframe's session → its window's (page) session

	mu       sync.Mutex
	launched time.Time // the last newWindow: its window may not have attached yet
	newTabAt time.Time
	closing  bool
	allGone  chan struct{}
	goneOnce sync.Once
}

// launchGrace: how long a window asked for may take to attach before "no windows" counts.
const launchGrace = 5 * time.Second

func (a *App) run() error {
	if detach() {
		return nil
	}
	a.quit = make(chan struct{})
	dir := a.opts.DataDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support", a.opts.ID, "lightron")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ln, other := claimInstance(dir, a.urlsIn(os.Args[1:]), a.second, a.quitting, a.opts.Logf)
	if other {
		// The running app can't bring itself forward (macOS 14 lets only the app in front hand activation over, and
		// the running one is in the background). This process was started by the person (they opened the app again):
		// asking LaunchServices to open the engine activates it, and the reopen it gets brings a window up (menu.m).
		// Only if the engine runs: not running (the first instance is still starting it), LaunchServices would start
		// it for this, as for a link (menu.m's coldStart), and that starts the app a second time
		if out, _ := exec.Command("lsappinfo", "find", "bundleid="+a.opts.ID+".engine").Output(); len(strings.TrimSpace(string(out))) > 0 {
			_ = exec.Command("open", "-b", a.opts.ID+".engine").Run()
		}
		return nil
	}
	if ln != nil {
		defer ln.Close()
	}

	entry := a.opts.URL
	// With a splash, URLFunc runs while the engine starts and a window shows the splash (useSplash)
	var later chan urlResult
	if a.opts.URLFunc != nil && useSplash(a.opts) {
		later = make(chan urlResult, 1)
		go func() {
			u, err := a.opts.URLFunc()
			later <- urlResult{u, err}
		}()
	} else if a.opts.URLFunc != nil {
		var err error
		if entry, err = a.opts.URLFunc(); err != nil {
			return err
		}
	}
	var srv *server
	if a.opts.Handler != nil {
		var err error
		if srv, err = serve(a.opts.Handler, a.opts.Logf); err != nil {
			return err
		}
		defer srv.close()
		entry = srv.entry
	}

	browser, err := macengine.FindBrowser()
	if err != nil {
		noBrowser(a.opts.Name)
		return ErrNoBrowser
	}
	exe := browser
	var icns []byte
	if len(a.opts.Icon) > 0 {
		img, err := pack.DecodePNG(a.opts.Icon)
		if err != nil {
			return fmt.Errorf("lightron: Options.Icon: %w", err)
		}
		icns = pack.ICNS(img)
	}
	lib, libErr := macmenu.Path(dir)
	brand := macengine.Brand{Name: a.opts.Name, ID: a.opts.ID + ".engine", ICNS: icns}
	if len(a.opts.Protocols) > 0 && libErr == nil {
		// Links of the app's schemes go to the engine (it is what runs). Started by one while the app isn't running,
		// the engine loads the menu library, which hands the link to the shell instead (menu.m's coldStart): the
		// shell's path is in <dir>/shell, written at each start (the app may have moved, been updated).
		brand.Schemes = a.opts.Protocols
		brand.Env = map[string]string{"DYLD_INSERT_LIBRARIES": lib, "LIGHTRON_COLD": "1"}
		if os.Getenv("LIGHTRON_DEBUG") != "" {
			brand.Env["LIGHTRON_MENU_LOG"] = filepath.Join(dir, "menu.log")
		}
		if self, err := os.Executable(); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "shell"), []byte(self), 0o644)
		}
	}
	if e, err := macengine.Prepare(browser, filepath.Join(dir, "engine"), brand, a.opts.Logf); err != nil {
		a.logf("engine: %v; running the browser as installed (its name shows in the Dock)", err)
	} else {
		exe = e
	}

	m := &mac{app: a, exe: exe, profile: filepath.Join(dir, "profile"), allGone: make(chan struct{})}
	a.setEngine(m)
	var env []string
	if exe != browser { // the menu library loads only into the re-signed copy
		if libErr != nil {
			a.logf("menu: %v", libErr)
		} else if mm, err := newMacMenu(m, filepath.Join(dir, "menu.sock")); err != nil {
			a.logf("menu: %v", err)
		} else {
			m.menu = mm
			env = []string{"DYLD_INSERT_LIBRARIES=" + lib, "LIGHTRON_MENU_SOCK=" + mm.sock}
			if len(a.opts.Protocols) > 0 {
				env = append(env, "LIGHTRON_SCHEMES="+strings.Join(a.opts.Protocols, ","))
			}
			if os.Getenv("LIGHTRON_DEBUG") != "" {
				env = append(env, "LIGHTRON_MENU_LOG="+filepath.Join(dir, "menu.log"))
			}
		}
	}

	var extra []string
	if later != nil {
		sp, err := serveSplash(a.opts.Splash, a.opts.Logf)
		if err != nil {
			return err
		}
		defer sp.Close()
		m.splash = "http://" + sp.Addr().String() + "/"
	} else {
		var pageEntry string
		pageEntry, extra = mapHost(entry, a.opts.Host)
		m.entry, m.origin = pageEntry, originOf(pageEntry)
	}
	extra = append(extra, a.opts.BrowserArgs...)
	m.waitProfileFree()
	chrome, proc, err := m.start(env, extra)
	if err != nil {
		return err
	}
	m.grantPermissions()
	if srv != nil {
		// The Handler's cookie goes into the browser over the pipe, and windows open on the plain page URL: the
		// login link would be on the command line that opens the window, which every process on the Mac can read.
		if _, err := m.c.Call("", "Storage.setCookies", map[string]any{"cookies": []map[string]any{{
			"name": cookieName, "value": srv.secret, "url": m.origin + "/", "httpOnly": true, "sameSite": "Strict",
		}}}); err != nil {
			return fmt.Errorf("lightron: set the page cookie: %w", err)
		}
		m.entry = m.origin + "/"
	}
	if later != nil {
		if err := m.newWindow(""); err != nil {
			return err
		}
		var r urlResult
		select {
		case r = <-later:
		case <-a.quit:
		case <-m.allGone:
		case <-chrome:
			a.logf("the browser quit before the app's pages were ready")
		}
		if r.err != nil || r.url == "" {
			_, _ = m.c.Call("", "Browser.close", nil)
			select {
			case <-chrome:
			case <-time.After(5 * time.Second):
				_ = proc.Kill()
			}
			return r.err // nil: closed before the pages were ready
		}
		m.waitWindow(launchGrace)
		m.setEntry(r.url)
	} else if err := m.newWindow(a.opts.Window.Path); err != nil {
		return err
	}
	go a.openURLs(a.urlsIn(os.Args[1:])) // started by a link (Windows; macOS: the engine started the app for one)
	if a.opts.OnReady != nil {
		go a.opts.OnReady(a)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-a.quit:
	case <-sig:
	case <-m.allGone:
	case <-chrome:
	}
	m.mu.Lock()
	m.closing = true
	m.mu.Unlock()
	if m.menu != nil {
		m.menu.quitting()
	}
	_, _ = m.c.Call("", "Browser.close", nil)
	select {
	case <-chrome:
	case <-time.After(5 * time.Second):
		// Browser.close didn't take: an engine left running with nobody to drive it would be an app in the Dock that
		// doesn't answer. (The menu library also quits it when our socket closes, a SIGKILL of us included.)
		_ = proc.Kill()
	}
	return nil
}

// start launches the engine with a CDP pipe and no window (newWindow opens --app ones), attached to every page
// before it runs.
func (m *mac) start(env, extra []string) (<-chan struct{}, *os.Process, error) {
	setPrefs(m.profile)
	args := append([]string{
		"--user-data-dir=" + m.profile,
		"--remote-debugging-pipe",
		"--no-startup-window",
		"--no-first-run",
		"--no-default-browser-check",
		// The engine copy is re-signed: the user's keychain item for the browser would prompt for it.
		"--use-mock-keychain",
		// A new profile says en-US whatever the system's language; pages follow navigator.languages.
		"--accept-lang=" + acceptLang(),
		// Chrome warns about some flags in a bar over the page; --test-type turns that off. Also on the command line
		// that opens each window (newWindow): the bars are decided on that one.
		"--test-type",
		// The size of a window the browser has no placement saved for: the first one, and new ones (they cascade
		// from the last). Once moved or resized, the browser restores that instead. Read from this process's command
		// line, not the one opening the window.
		fmt.Sprintf("--window-size=%d,%d", m.app.opts.Window.Width, m.app.opts.Window.Height),
	}, extra...)
	cmd := exec.Command(m.exe, args...)
	cmd.Env = append(os.Environ(), env...)
	r3, w3, _ := os.Pipe() // the browser reads commands from fd 3
	r4, w4, _ := os.Pipe() // and writes replies to fd 4
	cmd.ExtraFiles = []*os.File{r3, w4}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("lightron: start %s: %w", m.exe, err)
	}
	r3.Close()
	w4.Close()
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	m.c = cdp.New(w3, r4, m.event)
	if _, err := m.c.Call("", "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}); err != nil {
		_ = cmd.Process.Kill()
		return nil, nil, fmt.Errorf("lightron: the browser didn't answer: %w", err)
	}
	return exited, cmd.Process, nil
}

func (m *mac) event(msg *cdp.Msg) {
	switch msg.Method {
	case "Target.attachedToTarget":
		var p struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				URL      string `json:"url"`
				OpenerID string `json:"openerId"`
			} `json:"targetInfo"`
			Waiting bool `json:"waitingForDebugger"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		t := p.TargetInfo
		if t.Type == "iframe" {
			// Sent on the session of the page (or frame) it is in
			go m.attachFrame(p.SessionID, m.pageOf(msg.SessionID), p.Waiting)
			return
		}
		go m.attach(p.SessionID, t.TargetID, t.Type, t.URL, t.OpenerID, p.Waiting)
	case "Target.detachedFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		for _, w := range m.app.Windows() {
			if w.session == p.SessionID {
				m.app.removeWindow(w.id)
			}
		}
		m.ctxMu.Lock()
		for k := range m.origins {
			if strings.HasPrefix(k, p.SessionID+" ") {
				delete(m.origins, k)
			}
		}
		for f, page := range m.frames {
			if f == p.SessionID || page == p.SessionID {
				delete(m.frames, f)
			}
		}
		m.ctxMu.Unlock()
		m.checkGone()
	case "Runtime.executionContextCreated":
		var p struct {
			Context struct {
				ID     int64  `json:"id"`
				Origin string `json:"origin"`
			} `json:"context"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		m.ctxMu.Lock()
		if m.origins == nil {
			m.origins = map[string]string{}
		}
		m.origins[msg.SessionID+" "+itoa(p.Context.ID)] = p.Context.Origin
		m.ctxMu.Unlock()
	case "Runtime.bindingCalled":
		var p struct {
			Payload string
			Context int64 `json:"executionContextId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		// The binding is on every page the window shows; only the app's own pages may use it (Options.PageScript).
		m.ctxMu.Lock()
		origin := m.origins[msg.SessionID+" "+itoa(p.Context)]
		m.ctxMu.Unlock()
		if !appOrigin(origin, m.org(), m.app.opts.Origins) {
			m.app.logf("bridge: a call from %q ignored (not the app's page)", origin)
			return
		}
		if w := m.windowBySession(m.pageOf(msg.SessionID)); w != nil {
			go m.bridge(w, msg.SessionID, p.Payload)
		}
	case "Inspector.targetCrashed":
		// The page's process died (a crash, or macOS reclaimed it): load it again.
		m.app.logf("a page crashed: reloading it")
		go func(session string) {
			time.Sleep(500 * time.Millisecond)
			_, _ = m.c.Call(session, "Page.reload", nil)
		}(msg.SessionID)
	}
}

// pageOf: the page session a session is in (itself for a page, the window's page for an out-of-process iframe).
func (m *mac) pageOf(s string) string {
	m.ctxMu.Lock()
	defer m.ctxMu.Unlock()
	if p, ok := m.frames[s]; ok {
		return p
	}
	return s
}

func (m *mac) windowBySession(s string) *Window {
	for _, w := range m.app.Windows() {
		if w.session == s {
			return w
		}
	}
	return nil
}

func (m *mac) attach(session, target, typ, u, opener string, waiting bool) {
	run := func() {
		if waiting {
			_, _ = m.c.Call(session, "Runtime.runIfWaitingForDebugger", nil)
		}
	}
	if typ != "page" {
		run()
		_, _ = m.c.Call("", "Target.detachFromTarget", map[string]any{"sessionId": session})
		return
	}
	if opener == "" && newTabPage(u) {
		// The browser's new-tab page in a tabbed window (⌘N / ⌘T of the browser's own menu, a reopen the menu library
		// didn't take): not the app's. A window of the app's instead.
		run()
		_, _ = m.c.Call("", "Target.closeTarget", map[string]any{"targetId": target})
		m.mu.Lock()
		again := time.Since(m.newTabAt) < 2*time.Second
		m.newTabAt = time.Now()
		m.mu.Unlock()
		if !again {
			m.app.FocusOrOpen()
		}
		return
	}
	if opener != "" && !m.ours(u) {
		run()
		go m.popup(target)
		return
	}
	for _, step := range []struct {
		method string
		params any
	}{
		// Without Runtime.enable first, a binding added while the target waits for us is gone after its next
		// navigation (addBinding succeeds, window.__lightronPost is undefined).
		{"Runtime.enable", nil},
		{"Runtime.addBinding", map[string]any{"name": "__lightronPost"}},
		{"Page.enable", nil},
		{"Inspector.enable", nil},
		// (the page's own scripts: addPageScripts, below)
		// Iframes of another site run in their own process, which the page's scripts don't reach: attach them too
		// (attachFrame)
		{"Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}},
	} {
		if _, err := m.c.Call(session, step.method, step.params); err != nil {
			m.app.logf("attach %s: %v", step.method, err)
		}
	}
	if ua := m.userAgent(); ua != "" {
		if _, err := m.c.Call(session, "Emulation.setUserAgentOverride", map[string]any{"userAgent": ua}); err != nil {
			m.app.logf("attach user agent: %v", err)
		}
	}
	m.addPageScripts(session)
	m.app.addWindow(&Window{app: m.app, id: target, session: session})
	run()
	go m.ensureBridge(session)

}

// attachFrame: an iframe of another site than its page (out of process): the same scripts as a page, so an app's
// page embedded in a website (a panel docked in it) has window.lightron too. Only files' paths are answered there
// (bridge): invoke's replies and events go to the window's top page.
func (m *mac) attachFrame(session, page string, waiting bool) {
	m.ctxMu.Lock()
	if m.frames == nil {
		m.frames = map[string]string{}
	}
	m.frames[session] = page
	m.ctxMu.Unlock()
	for _, step := range []struct {
		method string
		params any
	}{
		{"Runtime.enable", nil},
		{"Runtime.addBinding", map[string]any{"name": "__lightronPost"}},
		{"Page.enable", nil},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": pageScripts(m.org(), m.app.opts)}},
		{"Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}},
	} {
		if _, err := m.c.Call(session, step.method, step.params); err != nil {
			m.app.logf("attach frame %s: %v", step.method, err)
		}
	}
	if waiting {
		_, _ = m.c.Call(session, "Runtime.runIfWaitingForDebugger", nil)
	}
}

// ensureBridge: a window's page must start with the bridge in place, and attaching early isn't enough: a login
// link's redirect is decided in the browser process, so its page can commit while attach is still adding the script.
// Whichever happened: once the page has loaded on the app's origin without the bridge, reload it, once.
func (m *mac) ensureBridge(session string) {
	for i := 0; i < 100; i++ {
		v, err := m.c.Eval(session, `[document.readyState, location.origin, !!window.lightron].join("|")`)
		if err != nil {
			return
		}
		var st string
		_ = json.Unmarshal(v, &st)
		parts := strings.Split(st, "|")
		if len(parts) == 3 && parts[2] == "true" {
			return
		}
		if len(parts) == 3 && parts[0] != "loading" && appOrigin(parts[1], m.org(), m.app.opts.Origins) {
			_, _ = m.c.Call(session, "Page.reload", nil)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// userAgent: the browser's User-Agent plus Options.UserAgentSuffix, asked once ("" without a suffix).
func (m *mac) userAgent() string {
	if m.app.opts.UserAgentSuffix == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ua == "" {
		r, err := m.c.Call("", "Browser.getVersion", nil)
		if err != nil {
			return ""
		}
		var v struct {
			UserAgent string `json:"userAgent"`
		}
		_ = json.Unmarshal(r, &v)
		if v.UserAgent == "" {
			return ""
		}
		m.ua = v.UserAgent + m.app.opts.UserAgentSuffix
	}
	return m.ua
}

func newTabPage(u string) bool {
	return strings.HasPrefix(u, "chrome://newtab") || strings.HasPrefix(u, "chrome://new-tab-page") || strings.HasPrefix(u, "chrome-search://")
}

func (m *mac) ours(u string) bool {
	o := m.org()
	if u == "" || u == "about:blank" || (o != "" && strings.HasPrefix(u, o+"/")) {
		return true
	}
	p, err := url.Parse(u)
	return err == nil && appOrigin(p.Scheme+"://"+p.Host, o, m.app.opts.Origins)
}

func (m *mac) ownOrigin() string { return m.org() }

// org / pages: the app's origin and entry (see mac.entry)
func (m *mac) org() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.origin
}

func (m *mac) pages() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entry
}

// addPageScripts: the bridge, Script and PageScript for the page of a window (pageScripts: the bridge only on the
// app's origins), its identifier kept so that setEntry can swap it.
func (m *mac) addPageScripts(session string) {
	r, err := m.c.Call(session, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": pageScripts(m.org(), m.app.opts)})
	if err != nil {
		m.app.logf("attach page scripts: %v", err)
		return
	}
	var res struct {
		Identifier string `json:"identifier"`
	}
	_ = json.Unmarshal(r, &res)
	m.mu.Lock()
	if m.scripts == nil {
		m.scripts = map[string]string{}
	}
	m.scripts[session] = res.Identifier
	m.mu.Unlock()
}

// setEntry: the app's pages are known (URLFunc returned while a splash showed): the windows' page scripts are made
// again for the app's origin, and the windows go there.
func (m *mac) setEntry(entry string) {
	m.mu.Lock()
	m.entry, m.origin = entry, originOf(entry)
	m.mu.Unlock()
	m.grantPermissions()
	for _, w := range m.app.Windows() {
		m.mu.Lock()
		old := m.scripts[w.session]
		m.mu.Unlock()
		if old != "" {
			_, _ = m.c.Call(w.session, "Page.removeScriptToEvaluateOnNewDocument", map[string]any{"identifier": old})
		}
		m.addPageScripts(w.session)
		if _, err := m.c.Call(w.session, "Page.navigate", map[string]any{"url": pageURL(entry, m.app.opts.Window.Path)}); err != nil {
			m.app.logf("splash: %v", err)
		}
		go m.ensureBridge(w.session)
	}
}

// grantPermissions: notifications and the clipboard without asking, on the app's origin. One by one:
// Browser.grantPermissions grants the ones listed and denies all others (copy buttons fail, the microphone is
// denied). The rest keep Chrome's default.
func (m *mac) grantPermissions() {
	o := m.org()
	if o == "" {
		return
	}
	for _, p := range []string{"notifications", "clipboard-read", "clipboard-write"} {
		_, _ = m.c.Call("", "Browser.setPermission", map[string]any{
			"origin": o, "permission": map[string]any{"name": p, "allowWithoutSanitization": p == "clipboard-write"}, "setting": "granted",
		})
	}
}

// popup: a window the page opened past the bridge's window.open (noopener…): once it knows where it goes, an
// outside address goes to the default browser instead.
func (m *mac) popup(target string) {
	for i := 0; i < 30; i++ {
		r, err := m.c.Call("", "Target.getTargetInfo", map[string]any{"targetId": target})
		if err != nil {
			return
		}
		var ti struct {
			TargetInfo struct{ URL string } `json:"targetInfo"`
		}
		_ = json.Unmarshal(r, &ti)
		if u := ti.TargetInfo.URL; u != "" && u != "about:blank" {
			if !m.ours(u) {
				_, _ = m.c.Call("", "Target.closeTarget", map[string]any{"targetId": target})
				openExternal(u)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// bridge: a message from window.lightron. Files' paths are the engine's to answer; the rest is the app's.
// session is the caller's: the window's page, or an iframe in it.
func (m *mac) bridge(w *Window, session, payload string) {
	var p struct {
		T  string `json:"t"`
		ID int64  `json:"id"`
	}
	if json.Unmarshal([]byte(payload), &p) == nil && p.T == "path" {
		path, _ := json.Marshal(m.filePath(session, p.ID))
		_, _ = m.c.Eval(session, "window.lightron.__settle("+itoa(p.ID)+","+string(path)+",null)")
		return
	}
	if session != w.session {
		// A link out (the bridge took the click over): nothing to answer, and only http(s) / mailto open
		if u, ok := externalOpen(payload); ok {
			go openExternal(u)
			return
		}
		// Anything else would be answered on the top page, whose calls are numbered on their own
		m.app.logf("bridge: only files' paths and links out are answered in an iframe")
		return
	}
	m.app.message(w, payload)
}

// filePath: the page parked the File in window.__lightronFiles; DOM.getFileInfo turns its remote object into a path.
func (m *mac) filePath(session string, id int64) string {
	r, err := m.c.Call(session, "Runtime.evaluate", map[string]any{"expression": "window.__lightronFiles.get(" + itoa(id) + ")"})
	if err != nil {
		return ""
	}
	var ev struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	_ = json.Unmarshal(r, &ev)
	if ev.Result.ObjectID == "" {
		return ""
	}
	r, err = m.c.Call(session, "DOM.getFileInfo", map[string]any{"objectId": ev.Result.ObjectID})
	if err != nil {
		return ""
	}
	var fi struct{ Path string }
	_ = json.Unmarshal(r, &fi)
	return fi.Path
}

// checkGone: no window left (closed, ⌘W) → quit, unless KeepRunning. Not while a window just asked for hasn't
// attached yet.
func (m *mac) checkGone() {
	m.mu.Lock()
	wait := launchGrace - time.Since(m.launched)
	closing := m.closing
	m.mu.Unlock()
	if closing || len(m.app.Windows()) > 0 || m.app.opts.KeepRunning {
		return
	}
	if wait > 0 {
		time.AfterFunc(wait, m.checkGone)
		return
	}
	m.mu.Lock()
	m.closing = true // from now on no window opens again (reopen, newWindow)
	m.mu.Unlock()
	m.app.closing.Store(true)
	if m.menu != nil {
		m.menu.quitting() // out of the Dock at once
	}
	m.goneOnce.Do(func() { close(m.allGone) })
}

// dockClick: a click on the Dock icon. Not while quitting (the browser is still shutting down: its icon may still be
// there), nor while a window asked for a moment ago hasn't attached yet (clicks in a row would open one each). Nor
// when one of the app's windows is on screen: activating the app has already brought it forward with its windows in
// their order, as any Mac app. Focusing one here picked any of them (Windows is a map), so with two windows a click
// on the Dock icon swapped them half the time.
func (m *mac) dockClick(visible bool) {
	m.mu.Lock()
	n := len(m.app.Windows())
	skip := m.closing || (n == 0 && time.Since(m.launched) < launchGrace) || (visible && n > 0)
	m.mu.Unlock()
	if !skip {
		m.app.FocusOrOpen()
	}
}

// reopen: the app was opened again (engine). Like a click on the Dock icon with no window on screen.
func (m *mac) reopen() { m.dockClick(false) }

// ---- engine ----

// newWindow: the engine started again with --app hands it to the running browser (its process singleton), which
// opens an app window: no tabs, no toolbar. CDP can't make one (Target.createTarget opens a tabbed window).
func (m *mac) newWindow(path string) error {
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return errors.New("lightron: the app is quitting")
	}
	m.launched = time.Now()
	m.mu.Unlock()
	entry := m.pages()
	if entry == "" {
		entry = m.splash
	}
	cmd := exec.Command(m.exe, "--user-data-dir="+m.profile, "--app="+pageURL(entry, path), "--test-type")
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (m *mac) call(w *Window, method string, params any) (json.RawMessage, error) {
	session := ""
	if w != nil {
		session = w.session
	}
	return m.c.Call(session, method, params)
}

func (m *mac) setMenu() {
	if m.menu != nil {
		m.menu.rebuild()
	}
}

func (m *mac) eval(w *Window, js string) error {
	_, err := m.c.Eval(w.session, js)
	return err
}

func (m *mac) focus(w *Window) {
	_, _ = m.c.Call("", "Target.activateTarget", map[string]any{"targetId": w.id})
	// activateTarget raises the window within the app; the app itself comes forward from inside (the menu library).
	// Never by `open`-ing the engine: that is a reopen, which the browser answers with a new window.
	if m.menu != nil {
		m.menu.activate()
	}
}

func (m *mac) close(w *Window) {
	_, _ = m.c.Call("", "Target.closeTarget", map[string]any{"targetId": w.id})
}

func (m *mac) setBadge(label string) {
	_, _ = m.c.Call("", "Browser.setDockTile", map[string]any{"badgeLabel": label})
}

// focusedWindow: the window with focus, else any (for menu items).
func (m *mac) focusedWindow() *Window {
	ws := m.app.Windows()
	for _, w := range ws {
		if v, err := m.c.Eval(w.session, "document.hasFocus()"); err == nil && string(v) == "true" {
			return w
		}
	}
	if len(ws) > 0 {
		return ws[0]
	}
	return nil
}

// ---- the profile ----

// setPrefs: profile preferences without a command-line switch, written while the browser isn't running (we hold the
// instance socket). The windows are an app, not a browser, so none of the browser's offers over the page:
// translate.enabled (it guesses a page's language and offers to translate it, in a bar), credentials_enable_service
// and profile.password_manager_enabled (a login form, or any form with a password field, gets "Save password?"),
// autofill.profile_enabled / credit_card_enabled (the same for addresses and cards).
func setPrefs(profile string) {
	p := filepath.Join(profile, "Default", "Preferences")
	prefs := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		if json.Unmarshal(b, &prefs) != nil {
			return
		}
	}
	changed := false
	for _, k := range []string{
		"translate.enabled",
		"credentials_enable_service",
		"profile.password_manager_enabled",
		"autofill.profile_enabled",
		"autofill.credit_card_enabled",
	} {
		if setPref(prefs, strings.Split(k, "."), false) {
			changed = true
		}
	}
	if !changed {
		return
	}
	if b, err := json.Marshal(prefs); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, b, 0o600)
	}
}

// setPref sets the value at a dotted path of Preferences' nested objects, creating them; true if it changed.
func setPref(m map[string]any, path []string, v any) bool {
	for _, k := range path[:len(path)-1] {
		next, _ := m[k].(map[string]any)
		if next == nil {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	last := path[len(path)-1]
	if m[last] == v {
		return false
	}
	m[last] = v
	return true
}

// acceptLang: the system's preferred languages (AppleLanguages: zh-Hans-CN, en-US…) as Accept-Language
// (zh-CN,zh,en-US,en). A new profile would say en-US whatever the system's language.
func acceptLang() string {
	// The user's list may be missing (never changed it), with the language only in the machine-wide preferences (set
	// at install) or in AppleLocale (region and language: zh-Hans_CN).
	var out []byte
	for _, args := range [][]string{
		{"read", "-g", "AppleLanguages"},
		{"read", "/Library/Preferences/.GlobalPreferences", "AppleLanguages"},
		{"read", "-g", "AppleLocale"},
	} {
		if b, err := exec.Command("defaults", args...).Output(); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			out = []byte(strings.ReplaceAll(string(b), "_", "-"))
			break
		}
	}
	var langs []string
	seen := map[string]bool{}
	add := func(l string) {
		if l != "" && !seen[l] {
			seen[l] = true
			langs = append(langs, l)
		}
	}
	for _, f := range strings.FieldsFunc(string(out), func(r rune) bool { return r == '(' || r == ')' || r == ',' || r == '\n' }) {
		l := strings.Trim(strings.TrimSpace(f), `"`)
		if l == "" {
			continue
		}
		parts := strings.Split(l, "-")
		lang := parts[0]
		region := ""
		for _, p := range parts[1:] {
			if len(p) == 2 && strings.ToUpper(p) == p { // a region (CN, US), not a script (Hans)
				region = p
			}
		}
		if region != "" {
			add(lang + "-" + region)
		}
		add(lang)
	}
	if len(langs) == 0 {
		return "en-US,en"
	}
	return strings.Join(langs, ",")
}

// noBrowser tells the user the app needs Chrome (or Edge, Brave), offering Chrome's download page.
func noBrowser(name string) {
	msg := name + " needs Google Chrome (or Microsoft Edge, Brave) on this Mac: it shows its window with that browser's engine instead of carrying one of its own.\n\nInstall Chrome, then open " + name + " again."
	script := `on run argv
display alert (item 1 of argv) message (item 2 of argv) buttons {"Quit", "Download Chrome"} default button 2
return button returned of result
end run`
	out, _ := exec.Command("osascript", "-e", script, name, msg).Output()
	if strings.TrimSpace(string(out)) == "Download Chrome" {
		_ = exec.Command("open", "https://www.google.com/chrome/").Run()
	}
}

// detach: started by LaunchServices (Finder, the Dock, Spotlight, a launcher: __CFBundleIdentifier is set), this
// program is recorded as the running app. Opening the app again then doesn't start the program: macOS sends the
// running one a reopen Apple event, which a Go program with no event loop never answers ("the application can no
// longer be opened", error -600). So the program starts a copy of itself that LaunchServices doesn't know about and
// leaves at once; every later open starts the program again, which hands over to the running one (claimInstance).
// true: this process should return now.
func detach() bool {
	if os.Getenv("__CFBundleIdentifier") == "" || os.Getenv("LIGHTRON_DETACHED") != "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil || !strings.Contains(exe, ".app/Contents/MacOS/") {
		return false
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "__CFBundleIdentifier=") {
			env = append(env, kv)
		}
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(env, "LIGHTRON_DETACHED=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return false // carry on in this process: only reopening is affected
	}
	_ = cmd.Process.Release()
	return true
}

// waitWindow: until a window has attached (the splash's: the browser takes a moment to open it), at most d.
func (m *mac) waitWindow(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end) && len(m.app.Windows()) == 0; {
		time.Sleep(50 * time.Millisecond)
	}
}

// waitProfileFree: a browser still on this profile (the app quit a moment ago and the browser is still shutting
// down) would take the one about to start: it hands over to the old one and exits. Wait for it, at most 15 s.
func (m *mac) waitProfileFree() {
	pat := "--user-data-dir=" + regexp.QuoteMeta(m.profile) + "( |$)"
	start := time.Now()
	for time.Since(start) < 15*time.Second {
		if exec.Command("pgrep", "-f", pat).Run() != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		m.app.logf("waited %v for the previous browser to quit", d.Round(100*time.Millisecond))
	}
}
