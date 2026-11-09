package lightron

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func TestServerOnlyAnswersItsWindows(t *testing.T) {
	s, err := serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "page "+r.URL.Path) }), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	base := strings.TrimSuffix(s.entry, loginPath+"?k="+s.secret)

	// Another process on the machine: no cookie, no page.
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("without the cookie: %d, want 403", resp.StatusCode)
	}
	// A wrong secret.
	resp, _ = http.Get(base + loginPath + "?k=nope")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong secret: %d, want 403", resp.StatusCode)
	}
	// The app's window: the login link, then the page it asked for.
	jar, _ := cookiejar.New(nil)
	c := http.Client{Jar: jar}
	resp, err = c.Get(pageURL(s.entry, "/notes/1"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "page /notes/1" {
		t.Fatalf("through the login link: %q", b)
	}
	// An open redirect isn't one.
	resp, _ = c.Get(base + loginPath + "?k=" + s.secret + "&next=//evil.example/")
	resp.Body.Close()
	if resp.Request.URL.Host != strings.TrimPrefix(base, "http://") {
		t.Fatalf("next=//evil went to %s", resp.Request.URL)
	}
}

func TestPageURL(t *testing.T) {
	for _, c := range []struct{ entry, path, want string }{
		{"http://127.0.0.1:5/__lightron/login?k=s", "", "http://127.0.0.1:5/__lightron/login?k=s"},
		{"http://127.0.0.1:5/__lightron/login?k=s", "/a?b=1", "http://127.0.0.1:5/__lightron/login?k=s&next=%2Fa%3Fb%3D1"},
		{"http://127.0.0.1:7870/auth?token=x", "", "http://127.0.0.1:7870/auth?token=x"},
		{"http://127.0.0.1:7870/auth?token=x", "/settings", "http://127.0.0.1:7870/settings"},
		{"https://example.com/app", "", "https://example.com/app"},
	} {
		if got := pageURL(c.entry, c.path); got != c.want {
			t.Errorf("pageURL(%q, %q) = %q, want %q", c.entry, c.path, got, c.want)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	for h, want := range map[string]bool{"127.0.0.1:80": true, "localhost": true, "[::1]:3000": true, "example.com": false, "10.0.0.1:80": false} {
		if got := isLoopback(h); got != want {
			t.Errorf("isLoopback(%q) = %v", h, got)
		}
	}
}
