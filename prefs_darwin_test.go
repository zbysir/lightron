package lightron

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// setPrefs turns the browser's offers off and keeps everything else in an existing profile.
func TestSetPrefs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Default", "Preferences")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte(`{"profile":{"name":"x","password_manager_enabled":true},"translate":{"enabled":true}}`), 0o600)
	setPrefs(dir)
	b, _ := os.ReadFile(p)
	var got struct {
		Profile struct {
			Name                   string `json:"name"`
			PasswordManagerEnabled *bool  `json:"password_manager_enabled"`
		} `json:"profile"`
		Translate struct {
			Enabled *bool `json:"enabled"`
		} `json:"translate"`
		Credentials *bool `json:"credentials_enable_service"`
		Autofill    struct {
			Profile *bool `json:"profile_enabled"`
			Card    *bool `json:"credit_card_enabled"`
		} `json:"autofill"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Profile.Name != "x" {
		t.Errorf("profile.name lost: %s", b)
	}
	for name, v := range map[string]*bool{
		"profile.password_manager_enabled": got.Profile.PasswordManagerEnabled,
		"translate.enabled":                got.Translate.Enabled,
		"credentials_enable_service":       got.Credentials,
		"autofill.profile_enabled":         got.Autofill.Profile,
		"autofill.credit_card_enabled":     got.Autofill.Card,
	} {
		if v == nil || *v {
			t.Errorf("%s not false: %s", name, b)
		}
	}
}
