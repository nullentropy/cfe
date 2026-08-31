package main

import "strconv"

var themes = map[string]map[string]string{
	// cyan; the default
	"KERNEL": {
		"bg": "#02070c", "ink": "#5fd8ff", "inkDim": "#37a5cd", "inkFaint": "#206682",
		"panel": "#06131d", "panelAlt": "#051019", "panelInset": "#040d14",
		"edge": "#1d5573", "edgeSoft": "#16405a", "titlebar": "#0a1e2c",
		"control": "#0c2434", "controlEdge": "#2a6b8f", "accent": "#00d0ff",
	},
	// green
	"DAEMON": {
		"bg": "#020604", "ink": "#3dff7c", "inkDim": "#1fce58", "inkFaint": "#12813a",
		"panel": "#06140c", "panelAlt": "#051009", "panelInset": "#030b06",
		"edge": "#14532d", "edgeSoft": "#0e3b20", "titlebar": "#082015",
		"control": "#0a2418", "controlEdge": "#1f6b3e", "accent": "#00ff66",
	},
	// amber
	"WARDEN": {
		"bg": "#0a0602", "ink": "#ffc24d", "inkDim": "#cd9430", "inkFaint": "#82601f",
		"panel": "#1a1206", "panelAlt": "#140e05", "panelInset": "#0f0a03",
		"edge": "#5c4415", "edgeSoft": "#443310", "titlebar": "#241906",
		"control": "#2a1e08", "controlEdge": "#7a5a1c", "accent": "#ffaa00",
	},
	// magenta
	"ORACLE": {
		"bg": "#080208", "ink": "#ff5fd2", "inkDim": "#c93da0", "inkFaint": "#7c2664",
		"panel": "#1a0714", "panelAlt": "#140510", "panelInset": "#0e040b",
		"edge": "#5c1e4a", "edgeSoft": "#43163a", "titlebar": "#240a1c",
		"control": "#2b0c21", "controlEdge": "#8a2c6b", "accent": "#ff2fbf",
	},
	// cyberpunk two-tone: pink towers over a cyan circuit floor (accent2)
	"GHOST": {
		"bg": "#0a0512", "ink": "#5fe6ff", "inkDim": "#37b3d4", "inkFaint": "#236e86",
		"panel": "#150a20", "panelAlt": "#0f0718", "panelInset": "#0a0512",
		"edge": "#6d2b9e", "edgeSoft": "#451c68", "titlebar": "#1a0b26",
		"control": "#221033", "controlEdge": "#9b3fd0", "accent": "#ff2e97", "accent2": "#12e2ff",
	},
}

var themeNames = []string{"KERNEL", "DAEMON", "WARDEN", "ORACLE", "GHOST"}

var metricsCRT = map[string]float64{
	"radius.control":     0,
	"radius.popover":     0,
	"radius.panel":       0,
	"control.height":     26,
	"control.padX":       12,
	"row.height":         22,
	"table.headerHeight": 24,
	"table.cellPadX":     8,
	"checkbox.size":      14,
	"slider.thumb":       7,
	"slider.track":       3,
	"dialog.titleHeight": 34,
	"space.pad":          8,
	"space.gap":          8,
}

// accentRGB feeds the Gibson shader: u_c* tints the towers, u_c*2 the circuit
// floor. accent2 defaults to accent, so a theme with one accent tints both.
func accentRGB(theme string) map[string]float64 {
	t := themes[theme]
	ch := func(hex string, i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		return float64(v) / 255.0
	}
	a, a2 := t["accent"], t["accent2"]
	if a2 == "" {
		a2 = a
	}
	return map[string]float64{
		"u_cr": ch(a, 1), "u_cg": ch(a, 3), "u_cb": ch(a, 5),
		"u_cr2": ch(a2, 1), "u_cg2": ch(a2, 3), "u_cb2": ch(a2, 5),
	}
}
