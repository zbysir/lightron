package lightron

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The pages of an Options.Handler, on a loopback port of their own. Any process on the machine can reach a loopback
// port, so the server only answers requests with a secret cookie, which the app's windows get from a login link with
// the secret in it (once per profile), and nothing else ever sees.

const (
	loginPath  = "/__lightron/login"
	cookieName = "lightron"
)

type server struct {
	ln     net.Listener
	secret string
	entry  string // the login link: http://127.0.0.1:<port>/__lightron/login?k=<secret>
}

func serve(h http.Handler, logf func(string, ...any)) (*server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	s := &server{ln: ln, secret: hex.EncodeToString(b)}
	s.entry = "http://" + ln.Addr().String() + loginPath + "?k=" + s.secret
	go func() {
		if err := http.Serve(ln, s.guard(h)); err != nil && !strings.Contains(err.Error(), "use of closed") {
			logf("lightron: server: %v", err)
		}
	}()
	return s, nil
}

func (s *server) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginPath {
			if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("k")), []byte(s.secret)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.secret, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			next := r.URL.Query().Get("next")
			if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
				next = "/"
			}
			http.Redirect(w, r, next, http.StatusFound)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *server) close() { _ = s.ln.Close() }

// pageURL: where a window on path opens, given the app's entry (a login link of ours, or Options.URL).
func pageURL(entry, path string) string {
	u, err := url.Parse(entry)
	if err != nil {
		return entry
	}
	if u.Path == loginPath { // ours: through the login link, so a fresh profile gets the cookie
		q := u.Query()
		if path != "" {
			q.Set("next", path)
		}
		u.RawQuery = q.Encode()
		return u.String()
	}
	if path == "" || path == "/" && u.Path != "" {
		return entry
	}
	return u.Scheme + "://" + u.Host + path
}

// isLoopback: a host that is this machine (127.x, ::1, localhost), the only kind the page host can be mapped to.
func isLoopback(host string) bool {
	h := host
	if x, _, err := net.SplitHostPort(host); err == nil {
		h = x
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
