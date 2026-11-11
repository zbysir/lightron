package lightron

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

// registerProtocols: HKCU\Software\Classes\<scheme> opens this executable with the link as its argument (a running
// app gets it from the second instance, single.go). Written at each start, so a moved or updated install still gets
// its links; no administrator rights needed.
func registerProtocols(name string, schemes []string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	set := func(path, key, value string) {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		_ = k.SetStringValue(key, value)
	}
	for _, s := range schemes {
		base := `Software\Classes\` + s
		set(base, "", "URL:"+name)
		set(base, "URL Protocol", "")
		set(base+`\DefaultIcon`, "", `"`+exe+`",0`)
		set(base+`\shell\open\command`, "", `"`+exe+`" "%1"`)
	}
}
