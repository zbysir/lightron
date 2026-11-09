package lightron

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func args(t *testing.T, vs ...any) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, v := range vs {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func TestBindings(t *testing.T) {
	type point struct{ X, Y int }
	var got *Window
	b, err := newBindings(map[string]any{
		"add":     func(a, b int) int { return a + b },
		"norm":    func(p point) (int, error) { return p.X*p.X + p.Y*p.Y, nil },
		"fail":    func() error { return errors.New("nope") },
		"nothing": func() {},
		"who":     func(w *Window, s string) string { got = w; return "hi " + s },
		"sum": func(xs ...int) int {
			n := 0
			for _, x := range xs {
				n += x
			}
			return n
		},
		"boom": func() int { panic("bad") },
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Window{id: "w1"}
	cases := []struct {
		name    string
		args    []json.RawMessage
		want    any
		wantErr string
	}{
		{"add", args(t, 2, 3), 5, ""},
		{"norm", args(t, point{3, 4}), 25, ""},
		{"fail", nil, nil, "nope"},
		{"nothing", nil, nil, ""},
		{"who", args(t, "you"), "hi you", ""},
		{"sum", args(t, 1, 2, 3), 6, ""},
		{"sum", nil, 0, ""},
		{"add", args(t, 1), nil, "takes 2 arguments"},
		{"add", args(t, "x", 1), nil, "argument 1"},
		{"missing", nil, nil, "no function"},
		{"boom", nil, nil, "panicked"},
	}
	for _, c := range cases {
		res, err := b.call(c.name, w, c.args)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s(%s): err %v, want %q", c.name, c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || res != c.want {
			t.Errorf("%s(%s) = %v, %v; want %v", c.name, c.args, res, err, c.want)
		}
	}
	if got != w {
		t.Error("the *Window parameter didn't get the calling window")
	}
}

func TestBindRejectsBadSignatures(t *testing.T) {
	for name, f := range map[string]any{
		"not a func":    42,
		"bad second":    func() (int, int) { return 0, 0 },
		"three results": func() (int, int, error) { return 0, 0, nil },
	} {
		if _, err := newBindings(map[string]any{"f": f}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDefaultHost(t *testing.T) {
	for id, want := range map[string]string{"com.example.Notes": "notes", "io.github.me.my_app": "myapp", "x": "x", "a.": "app"} {
		if got := defaultHost(id); got != want {
			t.Errorf("defaultHost(%q) = %q, want %q", id, got, want)
		}
	}
}
