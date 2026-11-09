package lightron

import "testing"

func TestAppOrigin(t *testing.T) {
	more := []string{"http://*.localhost:7891"}
	for _, c := range []struct {
		o  string
		ok bool
	}{
		{"http://127.0.0.1:7891", true},
		{"http://workspace.localhost:7891", true},
		{"http://a.b.localhost:7891", true},
		{"http://localhost:7891", false},
		{"http://evil.localhost:7892", false},
		{"https://workspace.localhost:7891", false},
		{"https://creght.cn", false},
		{"null", false},
	} {
		if got := appOrigin(c.o, "http://127.0.0.1:7891", more); got != c.ok {
			t.Errorf("appOrigin(%q) = %v, want %v", c.o, got, c.ok)
		}
	}
}
