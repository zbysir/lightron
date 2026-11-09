package lightron

import (
	"strings"
	"testing"
)

// acceptLang reads this Mac's settings: only checks that it gives a well-formed list.
func TestAcceptLang(t *testing.T) {
	l := acceptLang()
	t.Log(l)
	if l == "" || strings.ContainsAny(l, `"_ ()`) {
		t.Fatalf("acceptLang() = %q", l)
	}
}
