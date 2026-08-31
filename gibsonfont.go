package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	glyphW    = 7
	glyphH    = 13
	charsetLo = 32
	charsetHi = 95

	glyphIcons = 5
	// the shader derives its glyph-range check from FONT.length()/3, so adding
	// icons here keeps it in sync with no magic number on the GLSL side
	nGlyphs = 64 + glyphIcons

	towerTextCap = 120
)

var iconBitmaps = [glyphIcons][glyphH]string{
	{
		"    XX ", "    X X", "    X X", "    X  ", "    X  ", "    X  ",
		"    X  ", "  XXXX ", " XXXXX ", " XXXXX ", " XXXX  ", "  XX   ", "       ",
	},
	{
		"       ", "XXXXXXX", "X     X", "X X   X", "X     X", "X    XX",
		"X  XXXX", "X XXXXX", "XXXXXXX", "       ", "       ", "       ", "       ",
	},
	{
		"       ", "XXXXXXX", "X     X", "X X   X", "X XX  X", "X XXX X",
		"X XX  X", "X X   X", "X     X", "XXXXXXX", "       ", "       ", "       ",
	},
	{
		"       ", "XXXXXXX", "X     X", "XXXXXXX", "X  X  X", "X  X  X",
		"X     X", "X     X", "X     X", "XXXXXXX", "       ", "       ", "       ",
	},
	{
		"       ", "XXXXX  ", "X   XX ", "X    X ", "X XX X ", "X    X ",
		"X XX X ", "X    X ", "X XX X ", "XXXXXX ", "       ", "       ", "       ",
	},
}

var fontBits = buildFontBits()

// buildFontBits rasterizes the 7x13 font once and packs it the way the shader
// unpacks it: 91 bits per glyph (bit = row*7+col) into 3 uints. glyphs 0..63
// are ASCII from x/image's basicfont. 64.. are the hand-drawn filetype icons.
func buildFontBits() [nGlyphs * 3]uint32 {
	var out [nGlyphs * 3]uint32
	img := image.NewRGBA(image.Rect(0, 0, glyphW, glyphH))
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: basicfont.Face7x13,
	}
	for g := 0; g < 64; g++ {
		for i := range img.Pix {
			img.Pix[i] = 0
		}
		d.Dot = fixed.P(0, basicfont.Face7x13.Ascent-2)
		d.DrawString(string(rune(charsetLo + g)))
		for y := 0; y < glyphH; y++ {
			for x := 0; x < glyphW; x++ {
				if img.RGBAAt(x, y).A > 0x40 {
					bit := y*glyphW + x
					out[g*3+bit/32] |= 1 << (bit % 32)
				}
			}
		}
	}
	for k := range iconBitmaps {
		g := 64 + k
		for y := 0; y < glyphH; y++ {
			row := iconBitmaps[k][y]
			for x := 0; x < glyphW && x < len(row); x++ {
				if row[x] == 'X' {
					bit := y*glyphW + x
					out[g*3+bit/32] |= 1 << (bit % 32)
				}
			}
		}
	}
	return out
}

func fileIconCode(ext string) (byte, bool) {
	switch ext {
	case "mp3", "wav", "flac", "m4a", "aac", "ogg", "opus", "aiff", "aif", "wma":
		return 64, true
	case "jpg", "jpeg", "png", "gif", "webp", "heic", "heif", "bmp", "tiff", "tif", "svg", "ico":
		return 65, true
	case "mp4", "mov", "mkv", "avi", "webm", "m4v", "flv", "wmv", "mpg", "mpeg":
		return 66, true
	case "zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar", "dmg", "pkg", "jar":
		return 67, true
	case "pdf", "doc", "docx", "rtf", "epub", "mobi", "odt", "pages":
		return 68, true
	}
	return 0, false
}

func sanitizeGlyphs(s string, capN int) []byte {
	out := make([]byte, 0, capN)
	for _, r := range strings.ToUpper(s) {
		if len(out) >= capN {
			break
		}
		switch {
		case r >= charsetLo && r <= charsetHi:
			out = append(out, byte(r-charsetLo))
		case r == '\n' || r == '\t':
			out = append(out, ' '-charsetLo)
		default:
			out = append(out, '.'-charsetLo)
		}
	}
	return out
}

func towerText(dir string, t tower) string {
	full := filepath.Join(dir, t.name)
	var b strings.Builder
	b.WriteString(t.name)
	b.WriteString(" » ")
	switch t.kind {
	case kindPortal:
		b.Reset()
		b.WriteString("UPLINK » " + filepath.Dir(dir) + " ")
	case kindDir:
		if des, err := os.ReadDir(full); err == nil {
			for i, de := range des {
				if i >= 24 || b.Len() > towerTextCap*2 {
					break
				}
				b.WriteString(de.Name())
				b.WriteString(" · ")
			}
			if len(des) == 0 {
				b.WriteString("EMPTY SECTOR")
			}
		} else {
			b.WriteString("ACCESS DENIED")
		}
	case kindLink:
		if tgt, err := os.Readlink(full); err == nil {
			b.WriteString("-> " + tgt)
		}
	default:
		if !t.ent.mode.IsRegular() { // reading a FIFO/device could block the session goroutine
			b.WriteString("SPECIAL NODE")
			break
		}
		data, _, err := readCap(full, 256)
		if err != nil {
			b.WriteString("ACCESS DENIED")
		} else if isText(data) {
			b.WriteString(strings.Join(strings.Fields(string(data)), " "))
		} else {
			for i, by := range data {
				if i >= 64 {
					break
				}
				fmt.Fprintf(&b, "%02X ", by)
			}
		}
	}
	return b.String()
}

func towerGlyphs(dir string, t tower) []byte {
	if t.kind == kindFile && t.ent.mode.IsRegular() {
		if code, ok := fileIconCode(extOf(t.ent)); ok {
			const sp = byte(' ' - charsetLo)
			unit := []byte{code, sp, code, sp, code, sp}
			unit = append(unit, sanitizeGlyphs(t.name, towerTextCap)...)
			unit = append(unit, sp, sp)
			out := make([]byte, 0, towerTextCap)
			for len(out) < towerTextCap {
				out = append(out, unit...)
			}
			return out[:towerTextCap]
		}
	}
	g := sanitizeGlyphs(towerText(dir, t), towerTextCap)
	if len(g) == 0 {
		g = sanitizeGlyphs(t.name, towerTextCap)
	}
	return g
}

func packTowerText(dir string, towers []tower) (words []uint32, off, length []int) {
	var bytes []byte
	off = make([]int, len(towers))
	length = make([]int, len(towers))
	for i, t := range towers {
		g := towerGlyphs(dir, t)
		off[i] = len(bytes)
		length[i] = len(g)
		bytes = append(bytes, g...)
	}
	words = make([]uint32, (len(bytes)+3)/4)
	for i, by := range bytes {
		words[i/4] |= uint32(by) << (8 * (i % 4))
	}
	return
}

func glslUints(ws []uint32) (int, string) {
	n := max(len(ws), 1) // pad to >=1: zero-length arrays are illegal GLSL
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		var v uint32
		if i < len(ws) {
			v = ws[i]
		}
		fmt.Fprintf(&sb, "%du", v)
	}
	return n, sb.String()
}

func glslInts(vs []int) (int, string) {
	n := max(len(vs), 1)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		v := 0
		if i < len(vs) {
			v = vs[i]
		}
		fmt.Fprintf(&sb, "%d", v)
	}
	return n, sb.String()
}
