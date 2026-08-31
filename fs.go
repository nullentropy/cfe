package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// entry is one listing row. dir/link are the entry's own type, so a symlink to
// a directory reads as a link, not a dir
type entry struct {
	name string
	dir  bool
	link bool
	size int64
	mode fs.FileMode
	mod  time.Time
}

func readDir(path string) ([]entry, error) {
	des, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(des))
	for _, de := range des {
		e := entry{name: de.Name(), dir: de.IsDir()}
		if info, err := de.Info(); err == nil { // a failed stat degrades to zeros, not a failed listing
			e.size = info.Size()
			e.mode = info.Mode()
			e.mod = info.ModTime()
			e.link = info.Mode()&fs.ModeSymlink != 0
		}
		out = append(out, e)
	}
	sortEntries(out, "name", true)
	return out, nil
}

func sortEntries(es []entry, key string, asc bool) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.dir != b.dir {
			return a.dir
		}
		var less bool
		switch key {
		case "size":
			less = a.size < b.size
		case "mod":
			less = a.mod.Before(b.mod)
		case "type":
			less = extOf(a) < extOf(b)
		default:
			less = strings.ToLower(a.name) < strings.ToLower(b.name)
		}
		if !asc {
			less = !less
		}
		return less
	})
}

func extOf(e entry) string {
	if e.dir {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(e.name), "."))
}

func typeCell(e entry) string {
	switch {
	case e.link:
		return "LINK"
	case e.dir:
		return "DIR"
	}
	if x := extOf(e); x != "" {
		if len(x) > 6 {
			x = x[:6]
		}
		return strings.ToUpper(x)
	}
	return "FILE"
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

func readCap(path string, capN int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	buf := make([]byte, capN+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false, err
	}
	if n > capN {
		return buf[:capN], true, nil
	}
	return buf[:n], false, nil
}

func isText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	trim := b
	// tolerate a UTF-8 rune chopped off by readCap's byte limit
	for len(trim) > 0 && !utf8.Valid(trim) {
		trim = trim[:len(trim)-1]
		if len(b)-len(trim) > 3 {
			return false
		}
	}
	ctl := 0
	for _, r := range string(trim) {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			ctl++
		}
	}
	return ctl*100 < len(trim)+1
}

func hexDump(b []byte, width int) string {
	var sb strings.Builder
	for off := 0; off < len(b); off += width {
		end := min(off+width, len(b))
		fmt.Fprintf(&sb, "%06X  ", off)
		for i := off; i < off+width; i++ {
			if i < end {
				fmt.Fprintf(&sb, "%02X ", b[i])
			} else {
				sb.WriteString("   ")
			}
			if (i-off)%8 == 7 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte(' ')
		for i := off; i < end; i++ {
			c := b[i]
			if c < 0x20 || c > 0x7e {
				c = '.'
			}
			sb.WriteByte(c)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// clipLines caps a preview by line count and width
func clipLines(s string, maxLines, maxCols int) string {
	lines := strings.Split(s, "\n")
	clipped := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		clipped = true
	}
	for i, l := range lines {
		if utf8.RuneCountInString(l) > maxCols {
			r := []rune(l)
			lines[i] = string(r[:maxCols]) + "…"
		}
		lines[i] = strings.ReplaceAll(lines[i], "\t", "    ")
	}
	out := strings.Join(lines, "\n")
	if clipped {
		out += "\n…"
	}
	return out
}
