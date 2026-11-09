package pack

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Plist writes an Info.plist of string and boolean values.
func Plist(values map[string]any) []byte {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, k := range keys {
		fmt.Fprintf(&b, "  <key>%s</key>", html.EscapeString(k))
		switch v := values[k].(type) {
		case bool:
			if v {
				b.WriteString("<true/>\n")
			} else {
				b.WriteString("<false/>\n")
			}
		default:
			fmt.Fprintf(&b, "<string>%s</string>\n", html.EscapeString(fmt.Sprint(v)))
		}
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}
