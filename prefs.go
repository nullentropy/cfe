package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// prefs persists to os.UserConfigDir (~/Library/Application Support/cfe on
// macOS, ~/.config/cfe on Linux)
type prefs struct {
	Identity string             `json:"identity"`
	CRT      bool               `json:"crt"`
	Flicker  bool               `json:"flicker"`
	Sound    bool               `json:"sound"`
	Hidden   bool               `json:"hidden"`
	WinW     int                `json:"winW"`
	WinH     int                `json:"winH"`
	ColW     map[string]float64 `json:"colW"`
}

func defaultPrefs() prefs {
	return prefs{Identity: "KERNEL", CRT: true, Flicker: true, Hidden: false, WinW: 1280, WinH: 820}
}

func prefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "cfe", "prefs.json")
}

func loadPrefs() prefs {
	p := defaultPrefs()
	if data, err := os.ReadFile(prefsPath()); err == nil {
		_ = json.Unmarshal(data, &p) // a corrupt file just falls back to defaults
	}
	if _, ok := themes[p.Identity]; !ok {
		p.Identity = defaultPrefs().Identity
	}
	if p.WinW < 640 {
		p.WinW = defaultPrefs().WinW
	}
	if p.WinH < 480 {
		p.WinH = defaultPrefs().WinH
	}
	for k, w := range p.ColW { // guard against nonsense in a hand-edited file
		if w < 20 || w > 2000 {
			delete(p.ColW, k)
		}
	}
	return p
}

// savePrefs writes atomically (temp + rename) so a crash or a concurrent
// reader never sees a half-written file.
func savePrefs(p prefs) {
	path := prefsPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
