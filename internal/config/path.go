package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Path returns the config file location. The file need not exist.
//
// Resolution order:
//  1. STATUSLOOM_CONFIG environment variable (an explicit full file path).
//  2. $XDG_CONFIG_HOME/statusloom/config.json.
//  3. ~/.config/statusloom/config.json — used on both macOS and Linux.
//     This is XDG-style on macOS deliberately; statusloom does not use
//     ~/Library/Application Support.
//  4. On Windows: %AppData%\statusloom\config.json.
func Path() (string, error) {
	if p := os.Getenv("STATUSLOOM_CONFIG"); p != "" {
		return p, nil
	}

	if runtime.GOOS == "windows" {
		appData := os.Getenv("AppData")
		if appData == "" {
			return "", errors.New("config: %AppData% is not set")
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
