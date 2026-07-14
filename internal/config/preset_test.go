package config

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestBuiltInPresetMatchesDefault keeps presets/built-in/default.json and
// Default() from drifting apart: the file is loaded, defaulted, and
// compared against a freshly defaulted Default().
func TestBuiltInPresetMatchesDefault(t *testing.T) {
	data, err := os.ReadFile("../../presets/built-in/default.json")
	if err != nil {
		t.Fatalf("reading built-in preset: %v", err)
	}

	var fromFile Config
	if err := json.Unmarshal(data, &fromFile); err != nil {
		t.Fatalf("unmarshaling built-in preset: %v", err)
	}
	ApplyDefaults(&fromFile)

	want := Default()
	ApplyDefaults(want)

	if !reflect.DeepEqual(&fromFile, want) {
		t.Errorf("presets/built-in/default.json does not match Default():\ngot:  %+v\nwant: %+v", fromFile, *want)
	}
}
