package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	os.Exit(m.Run())
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.2.1", "1.2.0", true},
		{"1.2.0", "1.2.0", false},
		{"v1.10.0", "1.9.9", true},
		{"1.2.0-3-gabc1234", "1.2.0", true},
		{"1.2.1", "1.2.0-70-gabc1234-dirty", true},
		{"1.2.0", "1.2.0-3-gabc1234", false},
		{"0.1.0-dev", "0.0.1", false},
		{"1.0.0", "", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

type feedServer struct {
	*httptest.Server
	feedFails, fileFails int // answer 504 this many times first
	fileHits             int
}

func newFeed(t *testing.T, version, file string, body []byte, corrupt bool) *feedServer {
	t.Helper()
	sum := sha256.Sum256(body)
	served := body
	if corrupt {
		served = append([]byte{}, body...)
		served[0] ^= 0xff
	}
	feed, _ := json.Marshal(Feed{Version: version, Notes: "notes", Packages: map[string]Package{
		"darwin-arm64": {File: file, Size: int64(len(body)), Sha256: hex.EncodeToString(sum[:])},
	}})
	fs := &feedServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest/download/update.json", func(w http.ResponseWriter, r *http.Request) {
		if fs.feedFails > 0 {
			fs.feedFails--
			http.Error(w, "gateway", http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write(feed)
	})
	mux.HandleFunc("/releases/latest/download/"+file, func(w http.ResponseWriter, r *http.Request) {
		fs.fileHits++
		if fs.fileFails > 0 {
			fs.fileFails--
			http.Error(w, "gateway", http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write(served)
	})
	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func updater(t *testing.T, s *feedServer, current string) *Updater {
	return New(Options{FeedURL: s.URL + "/releases/latest/download/update.json", Current: current, Dir: t.TempDir(), Platform: "darwin-arm64", Logf: t.Logf})
}

func TestCheck(t *testing.T) {
	pkg := []byte("the new app")
	s := newFeed(t, "1.2.0", "App-1.2.0-darwin-arm64.dmg", pkg, false)

	if rel, err := updater(t, s, "1.2.0").Check(context.Background()); rel != nil || err != nil {
		t.Fatalf("up to date: %+v %v", rel, err)
	}
	u := updater(t, s, "1.1.0")
	rel, err := u.Check(context.Background())
	if err != nil || rel == nil || rel.Version != "1.2.0" || rel.Notes != "notes" {
		t.Fatalf("newer: %+v %v", rel, err)
	}
	if got, _ := os.ReadFile(rel.File); string(got) != string(pkg) {
		t.Fatalf("package: %q", got)
	}
	// Checked again before it's installed: not downloaded again.
	if _, err := u.Check(context.Background()); err != nil || s.fileHits != 1 {
		t.Fatalf("second check: %v, %d downloads", err, s.fileHits)
	}
}

func TestCheckRetriesServerErrors(t *testing.T) {
	s := newFeed(t, "1.2.0", "App.dmg", []byte("x"), false)
	s.feedFails, s.fileFails = 1, 2
	if rel, err := updater(t, s, "1.0.0").Check(context.Background()); err != nil || rel == nil {
		t.Fatalf("through 504s: %+v %v", rel, err)
	}
}

func TestCheckRejects(t *testing.T) {
	corrupt := newFeed(t, "1.2.0", "App.dmg", []byte("the new app"), true)
	if rel, err := updater(t, corrupt, "1.0.0").Check(context.Background()); err == nil || rel != nil {
		t.Fatalf("corrupt download: %+v %v", rel, err)
	}
	missing := newFeed(t, "1.2.0", "App.dmg", []byte("x"), false)
	u := New(Options{FeedURL: missing.URL + "/nope/update.json", Current: "1.0.0", Dir: t.TempDir(), Platform: "darwin-arm64"})
	if _, err := u.Check(context.Background()); err == nil {
		t.Fatal("404 feed: no error")
	}
	evil := newFeed(t, "1.2.0", "../../etc/x", []byte("x"), false)
	if _, err := updater(t, evil, "1.0.0").Check(context.Background()); err == nil {
		t.Fatal("a file name with a path: accepted")
	}
	if _, err := updater(t, missing, "0.1.0-dev").Check(context.Background()); !errors.Is(err, ErrNotUpdatable) {
		t.Fatalf("dev build: %v", err)
	}
	other := New(Options{FeedURL: missing.URL + "/releases/latest/download/update.json", Current: "1.0.0", Dir: t.TempDir(), Platform: "windows-amd64"})
	if _, err := other.Check(context.Background()); err == nil {
		t.Fatal("no package for the platform: no error")
	}
}
