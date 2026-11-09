// Package update keeps a Lightron app up to date from a feed: a JSON file listing the latest version and a package
// per platform (what `lightron feed` writes), e.g. the update.json of a GitHub release:
//
//	{"version": "1.2.0", "notes": "…", "packages": {
//	  "darwin-arm64":  {"file": "Notes-1.2.0-darwin-arm64.dmg",       "size": 18900000, "sha256": "…"},
//	  "windows-amd64": {"file": "Notes-1.2.0-windows-amd64-setup.exe", "size": 13900000, "sha256": "…"}}}
//
// Check downloads the newer package (checked: size, sha256; tried again when the server fails); Install puts it in
// place once the app has quit: macOS swaps the .app and opens the new one, Windows runs the installer silently.
//
//	u := update.New(update.Options{FeedURL: "https://github.com/me/notes/releases/latest/download/update.json",
//		Current: version, Dir: dataDir})
//	if rel, err := u.Check(ctx); err == nil && rel != nil {
//		if ask(rel.Version) && rel.Install() == nil {
//			app.Quit()
//		}
//	}
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Options configure an Updater.
type Options struct {
	// FeedURL is the feed's address. The packages are found next to it (same directory), so
	// …/releases/latest/download/update.json on GitHub works as is.
	FeedURL string
	// Current is this build's version: x.y.z, or a git describe (x.y.z-N-gabc, -dirty).
	Current string
	// Dir is where downloads wait until installed. Default: a directory under os.TempDir().
	Dir string
	// Platform is the feed's key for this build. Default: GOOS-GOARCH (darwin-arm64, windows-amd64).
	Platform string
	// Client makes the requests. Default: http.DefaultClient.
	Client *http.Client
	// Logf receives retries and the like. Default: log.Printf.
	Logf func(format string, args ...any)
}

// Feed is what a feed says.
type Feed struct {
	Version  string             `json:"version"`
	Notes    string             `json:"notes,omitempty"`
	Packages map[string]Package `json:"packages"`
}

// Package is one platform's file in a feed.
type Package struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

// Release is a newer version, downloaded and checked, waiting to be installed.
type Release struct {
	Version string
	Notes   string
	// File is the downloaded package.
	File string
}

// Updater checks a feed.
type Updater struct {
	o Options
}

// New returns an Updater.
func New(o Options) *Updater {
	if o.Platform == "" {
		o.Platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	if o.Dir == "" {
		o.Dir = filepath.Join(os.TempDir(), "lightron-update-"+strings.ReplaceAll(o.Platform, "/", "-"))
	}
	return &Updater{o: o}
}

// ErrNotUpdatable: Current isn't a version Check can compare (0.1.0-dev, empty). Such builds don't update.
var ErrNotUpdatable = errors.New("update: this build's version can't be compared with releases")

// Check reads the feed and, when it has a newer version with a package for this platform, downloads that. It
// returns nil, nil when up to date. A package downloaded before and not installed isn't downloaded again.
func (u *Updater) Check(ctx context.Context) (*Release, error) {
	if ParseVersion(u.o.Current) == nil {
		return nil, ErrNotUpdatable
	}
	var f Feed
	if err := u.retry(ctx, func() error { return u.getJSON(ctx, u.o.FeedURL, &f) }); err != nil {
		return nil, err
	}
	if !Newer(f.Version, u.o.Current) {
		return nil, nil
	}
	p, ok := f.Packages[u.o.Platform]
	if !ok {
		return nil, fmt.Errorf("update: %s has no package for %s", f.Version, u.o.Platform)
	}
	if strings.ContainsAny(p.File, `/\`) || p.File == "" {
		return nil, fmt.Errorf("update: bad file name %q", p.File)
	}
	file := filepath.Join(u.o.Dir, p.File)
	if st, err := os.Stat(file); err == nil && st.Size() == p.Size && sha(file) == p.Sha256 {
		return &Release{Version: f.Version, Notes: f.Notes, File: file}, nil
	}
	_ = os.RemoveAll(u.o.Dir)
	if err := os.MkdirAll(u.o.Dir, 0o755); err != nil {
		return nil, err
	}
	src, err := url.JoinPath(u.o.FeedURL, "..", p.File)
	if err != nil {
		return nil, err
	}
	if err := u.retry(ctx, func() error { return u.download(ctx, src, file+".part", p) }); err != nil {
		return nil, err
	}
	if err := os.Rename(file+".part", file); err != nil {
		return nil, err
	}
	return &Release{Version: f.Version, Notes: f.Notes, File: file}, nil
}

// retryDelays: the waits between tries. Release hosts time out now and then (GitHub: 504s for minutes); one failed
// try would otherwise end the check until the next one, hours later.
var retryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second}

// final: an error another try won't change (a 4xx: the file isn't there).
type final struct{ error }

func (u *Updater) retry(ctx context.Context, try func() error) error {
	err := try()
	for _, d := range retryDelays {
		var f final
		if err == nil || errors.As(err, &f) || ctx.Err() != nil {
			break
		}
		u.o.Logf("update: %v; again in %v", err, d)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		err = try()
	}
	var f final
	if errors.As(err, &f) {
		return f.error
	}
	return err
}

func statusErr(what string, resp *http.Response) error {
	err := fmt.Errorf("update: %s: %s", what, resp.Status)
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return err
	}
	return final{err}
}

func (u *Updater) getJSON(ctx context.Context, src string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", src, nil)
	if err != nil {
		return final{err}
	}
	resp, err := u.o.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return statusErr(src, resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return final{fmt.Errorf("update: %s: %w", src, err)}
	}
	return nil
}

func (u *Updater) download(ctx context.Context, src, dest string, p Package) error {
	req, err := http.NewRequestWithContext(ctx, "GET", src, nil)
	if err != nil {
		return final{err}
	}
	resp, err := u.o.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return statusErr(p.File, resp)
	}
	f, err := os.Create(dest)
	if err != nil {
		return final{err}
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	f.Close()
	if err != nil {
		os.Remove(dest)
		return err
	}
	// The feed and the file come from the same place: this catches truncation, not forgery.
	if (p.Size > 0 && n != p.Size) || (p.Sha256 != "" && hex.EncodeToString(h.Sum(nil)) != p.Sha256) {
		os.Remove(dest)
		return fmt.Errorf("update: %s: download corrupted (%d bytes)", p.File, n)
	}
	return nil
}

func sha(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

var verRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(\d+)-g[0-9a-f]+)?(?:-dirty)?$`)

// ParseVersion reads a release (x.y.z, v optional) or a git describe (x.y.z-N-gabc[-dirty]) as [x, y, z, N], N being
// commits past the x.y.z tag: a local build is ahead of the release it was built on and behind the next one. nil for
// anything else (0.1.0-dev).
func ParseVersion(v string) []int {
	m := verRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return nil
	}
	out := make([]int, 4)
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

// Newer reports whether version a is newer than b (false when either can't be read).
func Newer(a, b string) bool {
	x, y := ParseVersion(a), ParseVersion(b)
	if x == nil || y == nil {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}
