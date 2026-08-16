package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(Dir(), "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A definition says how a program works and is the same on every machine that
// runs it; config.toml says what this machine does differently. So config.toml
// is read last and a target with a definition's name replaces it whole.
func TestConfigOverridesADefinitionByName(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "kitty.toml", "[[targets]]\nname = 'kitty'\n[targets.detect]\ncommand = 'kitty'\n[[targets.reload]]\nsignal = 'USR1'\nprocess = 'kitty'\n")
	writeConfig(t, "[[targets]]\nname = 'kitty'\n[targets.detect]\ncommand = 'kitty'\n[[targets.reload]]\ncommand = ['true']\n\n[[targets]]\nname = 'extra'\n[targets.detect]\nalways = true\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Target{}
	for _, tgt := range cfg.Targets {
		byName[tgt.Name] = tgt
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d (%v), want the overridden one and the new one", len(cfg.Targets), byName)
	}
	kitty := byName["kitty"]
	if len(kitty.Ops) != 1 || kitty.Ops[0].Kind != OpCommand {
		t.Errorf("kitty ops = %v, want the config.toml reload to have replaced the definition's", kitty.Ops)
	}
	if _, ok := byName["extra"]; !ok {
		t.Error("a name config.toml alone declares did not extend the list")
	}
}

// An absent key keeps what it had: config.toml states what differs, not
// everything that is true.
func TestConfigOverlaysOnlyTheKeysItSets(t *testing.T) {
	withConfigDir(t)
	writeConfig(t, "themes_url = 'https://example.invalid/themes.txt'\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThemesURL != "https://example.invalid/themes.txt" {
		t.Errorf("themes_url = %q", cfg.ThemesURL)
	}
	if cfg.StateFile != Default().StateFile {
		t.Errorf("state_file = %q, want the default kept", cfg.StateFile)
	}
}

// No config.toml at all is the normal first run, not a failure.
func TestLoadWithoutAConfigFile(t *testing.T) {
	withConfigDir(t)
	if _, err := Load(); err != nil {
		t.Fatalf("a machine with no config.toml failed to load: %v", err)
	}
}

// A broken operation is reported at load, before a single file is touched,
// rather than halfway through a switch with the desktop already half changed.
func TestLoadRefusesABrokenOperation(t *testing.T) {
	withConfigDir(t)
	writeConfig(t, "[[targets]]\nname = 'broken'\n[targets.detect]\nalways = true\n[[targets.op]]\nkind = 'json-set'\nfile = 'x.json'\n")

	if _, err := Load(); err == nil {
		t.Fatal("a json-set with no key was accepted at load")
	}
}
