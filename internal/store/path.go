package store

// This file resolves the on-disk location of the single cross-tool
// configuration store (<configDir>/statusloom.json). The resolution rules are
// ported verbatim from internal/config (Path + configDir) so both packages
// agree on where statusloom keeps its files, but store deliberately does NOT
// import internal/config: the dependency direction is store -> dsl only, and
// config is a thin layer on top of store (config -> store), so a store ->
// config edge would be a cycle (see plans/config-store-and-format.md §8.11).

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// configFilePath returns the conventional config-file location, mirroring
// internal/config.Path. STATUSLOOM_CONFIG (an explicit path) wins; otherwise
// XDG / platform defaults apply. The file itself need not exist.
func configFilePath() (string, error) {
	if p := os.Getenv("STATUSLOOM_CONFIG"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("AppData")
		if appData == "" {
			return "", errors.New("store: %AppData% is not set")
		}
		return filepath.Join(appData, "statusloom", "config.json"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "statusloom", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "statusloom", "config.json"), nil
}

// configDir resolves the directory that holds statusloom's files, mirroring
// internal/config.configDir: when the resolved config path names an existing
// directory it is used verbatim (so a test setting STATUSLOOM_CONFIG=$(mktemp
// -d) keeps every file inside that directory), otherwise the parent directory
// of the config-file path is used.
func configDir() (string, error) {
	p, err := configFilePath()
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return p, nil
	}
	return filepath.Dir(p), nil
}

// StorePath returns the single cross-tool store location:
// <configDir>/statusloom.json. The file need not exist.
func StorePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "statusloom.json"), nil
}
