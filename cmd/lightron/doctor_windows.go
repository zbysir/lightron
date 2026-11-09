package main

import "golang.org/x/sys/windows/registry"

func runtimeChecks() {
	// WebView2's Evergreen runtime registers its version ("pv") under this client id: per machine (32-bit view) or
	// per user.
	const client = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, at := range []struct {
		root registry.Key
		path string
	}{{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\` + client}, {registry.LOCAL_MACHINE, `SOFTWARE\` + client}, {registry.CURRENT_USER, `Software\` + client}} {
		k, err := registry.OpenKey(at.root, at.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("pv")
		k.Close()
		if err == nil && v != "" && v != "0.0.0.0" {
			check(true, "WebView2", v)
			return
		}
	}
	check(false, "WebView2", "not found: apps open its download page (https://developer.microsoft.com/microsoft-edge/webview2/)")
}
