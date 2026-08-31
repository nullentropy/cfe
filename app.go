package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	caution "github.com/nullentropy/caution/go"
)

const helpText = `CFE DIRECTIVES
--------------------------------------------------
  cd <path>        jack into a sector (~, .., relative, absolute)
  <path>           a bare path that exists works too
  up | home | root parent / home / filesystem root
  find <text>      filter the current listing
  clear            drop the filter
  hidden           toggle dotfiles in the listing
  refresh | ls     re-scan the current sector
  pwd              where am i
  crt              toggle the tube
  gibson           fly the sector in 3D (cmd+G)
                   drag pans, scroll zooms, right-drag
                   orbits; click targets, double-click
                   jacks in; the obelisk goes up. cmd+L
                   opens a prompt: type a name to search
                   (next/prev to cycle). idle a moment and
                   the camera drifts - sometimes down the
                   streets between the towers
  help | ?         this transmission

IDENTITIES
--------------------------------------------------
  kernel | daemon | warden | oracle | ghost

MOUSE PROTOCOL
--------------------------------------------------
  OPEN>> / VIEW    enter a directory / scan a file
  back / forward   the mouse's extra buttons walk history
  right-click      context directives on the listing
  column headers   sort; drag dividers to resize
  sidebar          the map is not the territory
                   (dotfiles stay off the map)`

func fullA(inset float64) caution.A {
	return caution.A{
		Left: caution.Px(inset), Top: caution.Px(inset),
		Right: caution.Px(inset), Bottom: caution.Px(inset),
	}
}

func mount(s *caution.Session) *caution.Node {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/"
	}

	pf := loadPrefs()
	cur := "/"
	var history []string // visited sectors, for the mouse back/forward buttons
	histIdx := -1
	navigatingHist := false
	var entries []entry
	var rows []entry
	showHidden := pf.Hidden
	filter := ""
	sortKey, sortAsc := "name", true
	themeName := pf.Identity
	crtOn, flickerOn := pf.CRT, pf.Flicker
	winW, winH := pf.WinW, pf.WinH
	colW := pf.ColW
	if colW == nil {
		colW = map[string]float64{}
	}
	selKey := ""
	accessGen, hackGen := 0, 0
	virusRunning := false

	var nav func(path string) bool
	var appPane, root, statusRow *caution.Node
	var gib *gibson

	saveCh := make(chan prefs, 8)
	go func() {
		for {
			select {
			case <-s.Done():
				return
			case p := <-saveCh:
			drain:
				for {
					select {
					case p = <-saveCh:
					default:
						break drain
					}
				}
				savePrefs(p)
			}
		}
	}()

	persist := func() {
		cw := make(map[string]float64, len(colW))
		for k, v := range colW {
			cw[k] = v
		}
		select {
		case saveCh <- prefs{Identity: themeName, CRT: crtOn, Flicker: flickerOn,
			Hidden: showHidden, WinW: winW, WinH: winH, ColW: cw}:
		default:
		}
	}

	s.SetTheme(themes[themeName])
	s.SetMetrics(metricsCRT)
	s.SetTitle("CFE :: THE GIBSON")

	titleLbl := caution.Label("CFE :: FILE EXPLORER").Mono().FontSize(15).Weight(700).Color("$ink")
	fullscreen := s.Fullscreen()
	applyChrome := func() {
		left := 14.0
		if customChrome && !fullscreen {
			left = 84
		}
		titleLbl.Anchor(caution.A{Left: caution.Px(left), CenterY: caution.Px(0)})
		if gib != nil {
			gib.applyChrome()
		}
	}
	applyChrome()
	s.OnFullscreen(func(on bool) {
		fullscreen = on
		applyChrome()
	})
	clockLbl := caution.Label(time.Now().Format("15:04:05")).Mono().FontSize(13).Color("$inkDim").
		Anchor(caution.A{Right: caution.Px(14), Top: caution.Px(8)})
	dateLbl := caution.Label(strings.ToUpper(time.Now().Format("Mon 02 Jan 2006"))).Mono().FontSize(9).Color("$inkFaint").
		Anchor(caution.A{Right: caution.Px(14), Bottom: caution.Px(6)})

	pathLbl := caution.Label("/").Mono().FontSize(12).Color("$ink").Selectable().Fill(1)
	upBtn := caution.Button("UP").OnClick(func() { nav(filepath.Dir(cur)) })
	homeBtn := caution.Button("HOME").OnClick(func() { nav(home) })
	scanBtn := caution.Button("RE-SCAN").OnClick(func() { nav(cur) })

	statusLbl := caution.Label("BOOTING…").Mono().FontSize(10).Color("$inkDim")
	countsLbl := caution.Label("").Mono().FontSize(10).Color("$inkFaint")
	accessLbl := caution.Label("SECURE").Mono().FontSize(10).Weight(700).Color("$inkFaint")

	pvName := caution.Label("NO TARGET").Mono().FontSize(13).Weight(700).Color("$accent").Selectable()
	pvMeta := caution.Label("").Mono().FontSize(10).Color("$inkDim").Wrap()

	pvBody := caution.Label("SELECT AN OBJECT TO SCAN").Mono().FontSize(10).Color("$ink").Wrap().Selectable().
		Anchor(caution.A{Left: caution.Px(10), Top: caution.Px(10), Right: caution.Px(10)})

	cols := []caution.Col{
		{Key: "mark", Title: "", Width: 26},
		{Key: "name", Title: "NAME", Weight: 3},
		{Key: "type", Title: "TYPE", Width: 58},
		{Key: "size", Title: "SIZE", Width: 82},
		{Key: "mode", Title: "MODE", Width: 108},
		{Key: "mod", Title: "MODIFIED", Width: 136},
		{Key: "open", Title: "", Width: 80, Kind: "button"},
	}

	for i := range cols {
		if w, ok := colW[cols[i].Key]; ok {
			cols[i].Width, cols[i].Weight = w, 0
		}
	}
	table := caution.Table(cols, 0).RowKey(1)

	tree := caution.Tree([]caution.Col{{Key: "node", Title: "FILESYSTEM"}}, nil)

	gibsonFeed := caution.Shader(caution.FX{Frag: gibsonFrag, Animate: true, Uniforms: accentRGB(themeName)}).
		Anchor(fullA(0))

	cmdField := caution.TextField("").Mono().FontSize(12).
		Placeholder("TYPE A DIRECTIVE. TRY help, cd ~, find <text>").Fill(1)

	setStatus := func(msg string) { statusLbl.SetText(msg) }

	flashAccess := func(ok bool, msg string) {
		accessGen++
		gen := accessGen
		if ok {
			accessLbl.SetText("ACCESS GRANTED").SetColor("$accent")
		} else {
			accessLbl.SetText("ACCESS DENIED").SetColor("#ff3355")
			if msg != "" {
				setStatus(strings.ToUpper(msg))
			}
		}
		time.AfterFunc(2400*time.Millisecond, func() {
			s.Update(func() {
				if gen == accessGen {
					accessLbl.SetText("SECURE").SetColor("$inkFaint")
				}
			})
		})
	}

	resolve := func(p string) string {
		p = strings.TrimSpace(p)
		switch {
		case p == "~":
			return home
		case strings.HasPrefix(p, "~/"):
			return filepath.Join(home, p[2:])
		case filepath.IsAbs(p):
			return filepath.Clean(p)
		}
		return filepath.Join(cur, p)
	}

	entryByName := func(name string) (entry, bool) {
		for _, e := range rows {
			if e.name == name {
				return e, true
			}
		}
		return entry{}, false
	}

	rebuild := func() {
		list := make([]entry, 0, len(entries))
		for _, e := range entries {
			if !showHidden && strings.HasPrefix(e.name, ".") {
				continue
			}
			if filter != "" && !strings.Contains(strings.ToLower(e.name), filter) {
				continue
			}
			list = append(list, e)
		}
		sortEntries(list, sortKey, sortAsc)
		rows = rows[:0]
		if cur != "/" {
			rows = append(rows, entry{name: "..", dir: true})
		}
		rows = append(rows, list...)
		table.SetRowCount(len(rows))
		nd, nf, sz := 0, 0, int64(0)
		for _, e := range list {
			if e.dir {
				nd++
			} else {
				nf++
				sz += e.size
			}
		}
		c := fmt.Sprintf("%d DIR / %d FILE / %s", nd, nf, human(sz))
		if filter != "" {
			c = fmt.Sprintf("FILTER \"%s\" :: %s", strings.ToUpper(filter), c)
		}
		countsLbl.SetText(c)
	}

	showPreviewFor := func(key string) {
		if key == "" || key == ".." {
			pvName.SetText("NO TARGET")
			pvMeta.SetText("")
			pvBody.SetText("SELECT AN OBJECT TO SCAN")
			return
		}
		e, ok := entryByName(key)
		if !ok {
			return
		}
		full := filepath.Join(cur, e.name)
		pvName.SetText(strings.ToUpper(e.name))
		mod := "--"
		if !e.mod.IsZero() {
			mod = e.mod.Format("2006-01-02 15:04")
		}
		pvMeta.SetText(fmt.Sprintf("%s · %s · %s\n%s", typeCell(e), human(e.size), mod, e.mode.String()))
		if e.dir {
			des, err := os.ReadDir(full)
			if err != nil {
				pvBody.SetText("ACCESS DENIED\n" + strings.ToUpper(err.Error()))
				return
			}
			var b strings.Builder
			fmt.Fprintf(&b, "DIRECTORY NODE :: %d OBJECTS\n\n", len(des))
			for i, de := range des {
				if i >= 22 {
					b.WriteString("…\n")
					break
				}
				mark := "·"
				if de.IsDir() {
					mark = "»"
				}
				fmt.Fprintf(&b, "%s %s\n", mark, de.Name())
			}
			pvBody.SetText(b.String())
			return
		}
		data, trunc, err := readCap(full, 4096)
		if err != nil {
			pvBody.SetText("ACCESS DENIED\n" + strings.ToUpper(err.Error()))
			return
		}
		if isText(data) {
			txt := clipLines(string(data), 48, 160)
			if trunc {
				txt += "\n[ TRUNCATED :: VIEW FOR FULL SCAN ]"
			}
			pvBody.SetText(txt)
		} else {
			n := min(1024, len(data))
			pvBody.SetText("BINARY OBJECT :: HEX SCAN\n\n" + hexDump(data[:n], 8))
		}
	}

	openViewer := func(e entry, forceHex bool) {
		full := filepath.Join(cur, e.name)
		data, trunc, err := readCap(full, 64<<10)
		if err != nil {
			flashAccess(false, err.Error())
			return
		}
		var body string
		if !forceHex && isText(data) {
			body = clipLines(string(data), 2000, 400)
		} else {
			if len(data) > 8<<10 {
				data, trunc = data[:8<<10], true
			}
			body = hexDump(data, 16)
		}
		if trunc {
			body += "\n[ TRANSMISSION TRUNCATED ]"
		}
		meta := fmt.Sprintf("%s · %s · %s", full, human(e.size), e.mode.String())
		dlg := caution.Dialog(strings.ToUpper(e.name)).CardSize(860, 560)
		dlg.OnDismiss(func() { dlg.Remove() })
		dlg.Kids(caution.DockPanel().Anchor(fullA(0)).Kids(
			caution.VStack().Pad(10).Dock("top").Kids(
				caution.Label(meta).Mono().FontSize(10).Color("$inkDim").Selectable()),
			caution.Scroll().Kids(
				caution.Label(body).Mono().FontSize(11).Wrap().Selectable().
					Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(4), Right: caution.Px(12)})),
		))
		root.Add(dlg)
	}

	activate := func(key string, forceHex bool) {
		if key == "" {
			return
		}
		if key == ".." {
			nav(filepath.Dir(cur))
			return
		}
		e, ok := entryByName(key)
		if !ok {
			return
		}
		full := filepath.Join(cur, e.name)
		if e.dir {
			nav(full)
			return
		}
		if fi, err := os.Stat(full); err == nil && fi.IsDir() {
			nav(full)
			return
		}
		openViewer(e, forceHex)
	}

	type dnode struct {
		name   string
		loaded bool
		kids   []*dnode
	}
	rootD := &dnode{name: "/"}
	lookupD := func(path string) *dnode {
		if path == "/" {
			return rootD
		}
		d := rootD
		for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
			var next *dnode
			for _, k := range d.kids {
				if k.name == seg {
					next = k
					break
				}
			}
			if next == nil {
				return nil
			}
			d = next
		}
		return d
	}
	loadD := func(dn *dnode, path string) error {
		des, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		dn.kids = dn.kids[:0]
		for _, de := range des {
			if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
				continue
			}
			dn.kids = append(dn.kids, &dnode{name: de.Name()})
		}
		sort.Slice(dn.kids, func(i, j int) bool {
			return strings.ToLower(dn.kids[i].name) < strings.ToLower(dn.kids[j].name)
		})
		dn.loaded = true
		return nil
	}
	var buildItems func(dn *dnode, path string) []caution.TreeItem
	buildItems = func(dn *dnode, path string) []caution.TreeItem {
		items := make([]caution.TreeItem, 0, len(dn.kids))
		for _, k := range dn.kids {
			p := filepath.Join(path, k.name)
			it := caution.TreeItem{Key: p, Cells: []string{k.name}}
			if k.loaded {
				it.Kids = buildItems(k, p)
			} else {
				it.Kids = []caution.TreeItem{{Key: p + "\x00", Cells: []string{"…"}}}
			}
			items = append(items, it)
		}
		return items
	}
	refreshTree := func() {
		tree.SetTreeItems([]caution.TreeItem{{Key: "/", Cells: []string{"/"}, Kids: buildItems(rootD, "/")}})
	}
	expandTo := func(path string) {
		if loadD(rootD, "/") != nil {
			return
		}
		keys := []string{"/"}
		d, p := rootD, "/"
		for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
			if seg == "" {
				break
			}
			var next *dnode
			for _, k := range d.kids {
				if k.name == seg {
					next = k
					break
				}
			}
			if next == nil {
				break
			}
			p = filepath.Join(p, seg)
			if !next.loaded && loadD(next, p) != nil {
				break
			}
			keys = append(keys, p)
			d = next
		}
		refreshTree()
		tree.Expand(keys...)
	}

	nav = func(path string) bool {
		path = filepath.Clean(path)
		ents, err := readDir(path)
		if err != nil {
			flashAccess(false, err.Error())
			return false
		}
		cur = path
		if !navigatingHist && (histIdx < 0 || history[histIdx] != path) {
			history = append(history[:histIdx+1], path) // a new nav drops any forward entries
			histIdx = len(history) - 1
		}
		entries = ents
		filter = ""
		selKey = ""
		rebuild()
		pathLbl.SetText(path)
		table.SetSelectedKey("")
		showPreviewFor("")
		tree.SetSelectedKey(path)
		flashAccess(true, "")
		setStatus(fmt.Sprintf("SECTOR SCANNED :: %d OBJECTS", len(ents)))
		if gib != nil && gib.isOpen() {
			gib.refresh()
		}
		return true
	}

	applyTheme := func(name string) {
		if _, ok := themes[name]; !ok {
			return
		}
		themeName = name
		s.SetTheme(themes[name])
		gibsonFeed.SetUniforms(accentRGB(name))
		if gib != nil {
			gib.setTint(name)
		}
		setStatus("IDENTITY: " + name)
		persist()
	}
	crtState := func() (frag string, animate bool, curve float64) {
		switch {
		case !crtOn:
			return crtPass, false, 0
		case flickerOn:
			return crtFlicker, true, crtCurve
		default:
			return crtStatic, false, crtCurve
		}
	}
	applyCRT := func() {
		frag, animate, _ := crtState()
		appPane.SetProp("effect", map[string]any{"frag": frag, "animate": animate})
		if gib != nil {
			gib.applyCrt(crtState())
		}
	}

	showHelp := func() {
		dlg := caution.Dialog("DIRECTIVES").CardSize(600, 520)
		dlg.OnDismiss(func() { dlg.Remove() })
		dlg.Kids(caution.Scroll().Anchor(fullA(0)).Kids(
			caution.Label(helpText).Mono().FontSize(11).Wrap().Selectable().
				Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(14), Right: caution.Px(16)})))
		root.Add(dlg)
	}

	hackThePlanet := func() {
		hackGen++
		gen := hackGen
		go func() {
			for i := 0; i < 12; i++ {
				select {
				case <-s.Done():
					return
				case <-time.After(180 * time.Millisecond):
				}
				odd := i%2 == 1
				s.Update(func() {
					if gen != hackGen {
						return
					}
					c := "$accent"
					if odd {
						c = "#ff2fbf"
					}
					titleLbl.SetText("HACK THE PLANET! HACK THE PLANET!").SetColor(c)
				})
			}
			s.Update(func() {
				if gen != hackGen {
					return
				}
				titleLbl.SetText("CFE :: FILE EXPLORER").SetColor("$ink")
				setStatus("HACK THE PLANET.")
			})
		}()
	}

	daVinci := func() {
		if virusRunning {
			return
		}
		virusRunning = true
		bar := caution.Progress(0).W(140).Fixed(140)
		statusRow.Add(bar)
		setStatus("DA VINCI VIRUS DETECTED :: DISPATCHING FLU SHOT")
		go func() {
			for i := 0; i <= 24; i++ {
				select {
				case <-s.Done():
					return
				case <-time.After(110 * time.Millisecond):
				}
				v := float64(i) / 24
				s.Update(func() { bar.SetProgress(v) })
			}
			s.Update(func() {
				bar.Remove()
				virusRunning = false
				setStatus("DA VINCI PURGED :: TANKERS STABILIZED")
				flashAccess(true, "")
			})
		}()
	}

	var rebuildMenu func()
	rebuildMenu = func() {
		onoff := func(b bool) string {
			if b {
				return "ON"
			}
			return "OFF"
		}
		themeItems := make([]caution.MenuItem, 0, len(themeNames))
		for _, n := range themeNames {
			themeItems = append(themeItems, caution.MenuItem{Title: n, OnPick: func() { applyTheme(n) }})
		}
		s.SetMenu(
			caution.Menu{Title: "FILE", Items: []caution.MenuItem{
				{Title: "UP ONE LEVEL", Key: "u", OnPick: func() { nav(filepath.Dir(cur)) }},
				{Title: "HOME SECTOR", Key: "shift+cmd+g", OnPick: func() { nav(home) }},
				{Title: "ROOT SECTOR", Key: "shift+cmd+r", OnPick: func() { nav("/") }},
				{Sep: true},
				{Title: "RE-SCAN", Key: "r", OnPick: func() { nav(cur) }},
			}},
			caution.Menu{Title: "VIEW", Items: []caution.MenuItem{
				{Title: "GIBSON MODE", Key: "g", OnPick: func() {
					if gib != nil {
						gib.toggle()
					}
				}},
				{Sep: true},
				{Title: "DOTFILES: " + onoff(showHidden), Key: "i", OnPick: func() {
					showHidden = !showHidden
					rebuild()
					rebuildMenu()
					persist()
				}},
				{Sep: true},
				{Title: "CRT: " + onoff(crtOn), Key: "e", OnPick: func() {
					crtOn = !crtOn
					applyCRT()
					rebuildMenu()
					persist()
				}},
				{Title: "TUBE FLICKER: " + onoff(flickerOn), OnPick: func() {
					flickerOn = !flickerOn
					applyCRT()
					rebuildMenu()
					persist()
				}},
			}},
			caution.Menu{Title: "IDENTITY", Items: themeItems},
			caution.Menu{Title: "HELP", Items: []caution.MenuItem{
				{Title: "DIRECTIVES", OnPick: showHelp},
			}},
		)
	}

	runCmd := func(line string) {
		low := strings.ToLower(strings.TrimSpace(line))
		if low == "" {
			return
		}
		for _, n := range themeNames {
			if low == strings.ToLower(n) {
				applyTheme(n)
				return
			}
		}
		switch low {
		case "crash override":
			applyTheme("DAEMON")
			setStatus("IDENTITY: DAEMON // WE KNOW WHO YOU ARE, MR. MURPHY")
			return
		case "hack the planet", "hack the planet!":
			hackThePlanet()
			return
		case "god", "love", "sex", "secret":
			flashAccess(false, "")
			setStatus("THE FOUR MOST COMMON PASSWORDS ARE NOT DIRECTIVES, JOEY")
			return
		case "mess with the best":
			setStatus("DIE LIKE THE REST.")
			return
		case "gibson":
			if gib != nil {
				gib.toggle()
			}
			flashAccess(true, "")
			setStatus("YOU'RE IN.")
			return
		case "rabbit", "da vinci", "davinci":
			daVinci()
			return
		}

		fields := strings.Fields(line)
		arg := ""
		if len(fields) > 1 {
			arg = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		}
		switch strings.ToLower(fields[0]) {
		case "cd":
			if arg == "" {
				nav(home)
			} else {
				nav(resolve(arg))
			}
			return
		case "up":
			nav(filepath.Dir(cur))
			return
		case "home":
			nav(home)
			return
		case "root":
			nav("/")
			return
		case "ls", "refresh", "rescan":
			nav(cur)
			return
		case "pwd":
			setStatus("SECTOR: " + cur)
			return
		case "find", "filter", "grep":
			filter = strings.ToLower(arg)
			rebuild()
			setStatus("FILTER ENGAGED: \"" + strings.ToUpper(arg) + "\"")
			return
		case "clear":
			filter = ""
			rebuild()
			setStatus("FILTER DROPPED")
			return
		case "hidden", "dotfiles":
			showHidden = !showHidden
			rebuild()
			rebuildMenu()
			persist()
			return
		case "crt":
			crtOn = !crtOn
			applyCRT()
			rebuildMenu()
			persist()
			return
		case "help", "?":
			showHelp()
			return
		}
		if p := resolve(strings.TrimSpace(line)); func() bool {
			fi, err := os.Stat(p)
			return err == nil && fi.IsDir()
		}() {
			nav(p)
			return
		}
		setStatus("UNRECOGNIZED DIRECTIVE: " + strings.ToUpper(line))
	}

	table.RowsFunc(func(start, end int) [][]string {
		out := make([][]string, 0, max(0, end-start+1))
		for i := start; i <= end && i < len(rows); i++ {
			e := rows[i]
			mark, size, mode, mod, act, typ := "·", "--", e.mode.String(), "--", "VIEW", typeCell(e)
			if e.dir {
				mark, act = "»", "OPEN>>"
			}
			if e.name == ".." {
				mark, typ, mode, act = "«", "UP", "--", "OPEN>>"
			}
			if !e.dir {
				size = human(e.size)
			}
			if !e.mod.IsZero() {
				mod = e.mod.Format("2006-01-02 15:04")
			}
			out = append(out, []string{mark, e.name, typ, size, mode, mod, act})
		}
		return out
	})
	table.OnRowSelectKey(func(key string, row int) {
		selKey = key
		showPreviewFor(key)
	})
	table.OnRowActivateKey(func(key string, row int) {
		activate(key, false)
	})
	table.OnSort(func(key string, asc bool) {
		switch key {
		case "name", "type", "size", "mod":
			sortKey, sortAsc = key, asc
		default:
			sortKey, sortAsc = "name", asc
		}
		rebuild()
	})
	table.OnCellActivate(func(row int, key, col, value string) {
		if col == "open" {
			activate(key, false)
		}
	})
	table.OnColResize(func(key string, width float64) {
		colW[key] = width
		persist()
	})
	table.Context(
		caution.ContextItem{Title: "OPEN", OnPick: func() { activate(selKey, false) }},
		caution.ContextItem{Title: "HEX DUMP", OnPick: func() {
			if e, ok := entryByName(selKey); ok && !e.dir {
				openViewer(e, true)
			}
		}},
		caution.ContextItem{Sep: true},
		caution.ContextItem{Title: "RE-SCAN SECTOR", OnPick: func() { nav(cur) }},
	)
	tree.OnRowToggle(func(key string, expanded bool) {
		if !expanded || strings.HasSuffix(key, "\x00") {
			return
		}
		dn := lookupD(key)
		if dn == nil {
			return
		}
		if !dn.loaded {
			if err := loadD(dn, key); err != nil {
				tree.Collapse(key)
				flashAccess(false, err.Error())
				return
			}
			refreshTree()
		}
	})
	tree.OnRowSelectKey(func(key string, row int) {
		if !strings.HasSuffix(key, "\x00") {
			nav(key)
		}
	})
	cmdField.OnCommit(func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		cmdField.ClearValue()
		runCmd(v)
	})
	s.OnKey("cmd+l", func() {
		if gib != nil && gib.isOpen() {
			gib.focusCmd()
		} else {
			cmdField.Focus()
		}
	})

	s.OnAux(func(button int) {
		switch button {
		case 4:
			if histIdx > 0 {
				navigatingHist = true
				if nav(history[histIdx-1]) {
					histIdx--
				}
				navigatingHist = false
			}
		case 5:
			if histIdx < len(history)-1 {
				navigatingHist = true
				if nav(history[histIdx+1]) {
					histIdx++
				}
				navigatingHist = false
			}
		}
	})

	header := caution.Panel().Bg("$titlebar").Border("$edge", 1).H(44).Dock("top").WindowDrag().
		Kids(titleLbl, clockLbl, dateLbl)
	pathBar := caution.Panel().Bg("$panelAlt").Border("$edgeSoft", 1).H(36).Dock("top").Kids(
		caution.HStack().Gap(8).Align("center").Anchor(caution.A{
			Left: caution.Px(10), Right: caution.Px(6), Top: caution.Px(0), Bottom: caution.Px(0)}).Kids(
			caution.Label("PATH://").Mono().FontSize(12).Weight(700).Color("$accent"),
			pathLbl, upBtn, homeBtn, scanBtn))
	statusRow = caution.HStack().Gap(14).Align("center").Anchor(caution.A{
		Left: caution.Px(10), Right: caution.Px(10), Top: caution.Px(0), Bottom: caution.Px(0)}).Kids(
		statusLbl, caution.Panel().Fill(1), countsLbl, accessLbl)
	statusBar := caution.Panel().Bg("$titlebar").Border("$edge", 1).H(26).Dock("bottom").Kids(statusRow)
	cmdBar := caution.Panel().Bg("$panelAlt").Border("$edgeSoft", 1).H(36).Dock("bottom").Kids(
		caution.HStack().Gap(8).Align("center").Anchor(caution.A{
			Left: caution.Px(10), Right: caution.Px(10), Top: caution.Px(0), Bottom: caution.Px(0)}).Kids(
			caution.Label("CFE>").Mono().FontSize(12).Weight(700).Color("$accent"),
			cmdField,
			caution.Label("CMD+L").Mono().FontSize(9).Color("$inkFaint")))

	gibsonPanel := caution.Panel().Border("$edge", 1).H(170).Dock("bottom").Kids(
		gibsonFeed,
		caution.Label("THE GIBSON :: LIVE FEED").Mono().FontSize(9).Color("$accent").
			Anchor(caution.A{Left: caution.Px(8), Bottom: caution.Px(6)}))
	sidebar := caution.Panel().Bg("$panelInset").Border("$edgeSoft", 1).Kids(
		caution.DockPanel().Anchor(fullA(1)).Kids(gibsonPanel, tree))

	tablePanel := caution.Panel().Bg("$panelInset").Border("$edgeSoft", 1).Kids(
		table.Anchor(fullA(1)))
	preview := caution.Panel().Bg("$panelInset").Border("$edgeSoft", 1).Kids(
		caution.DockPanel().Anchor(fullA(1)).Kids(
			caution.VStack().Pad(10).Gap(6).Dock("top").Kids(
				caution.Label("PREVIEW //").Mono().FontSize(9).Color("$inkFaint"),
				pvName, pvMeta),
			caution.Scroll().Kids(pvBody)))

	mainSplit := caution.HSplit().SplitPos(660).SplitMin(380, 240).Kids(tablePanel, preview)
	body := caution.HSplit().SplitPos(260).SplitMin(190, 480).Kids(sidebar, mainSplit)

	appPane = caution.DockPanel().Anchor(fullA(0)).Kids(header, pathBar, statusBar, cmdBar, body)
	root = caution.Panel().Bg("$bg").Kids(appPane)

	gib = newGibson(gibsonDeps{
		s:     s,
		root:  root,
		nav:   func(p string) bool { return nav(p) },
		view:  func(e entry, hex bool) { openViewer(e, hex) },
		cur:   func() string { return cur },
		ents:  func() []entry { return rows },
		theme: func() string { return themeName },
		onTarget: func(name string) {
			selKey = name
			table.SetSelectedKey(name)
			showPreviewFor(name)
		},
		crt:        crtState,
		cmd:        runCmd,
		fullscreen: func() bool { return fullscreen },
	})
	s.OnResize(func(w, h float64) {
		if gib.isOpen() {
			gib.relayout()
		}
		if w >= 640 && h >= 480 {
			winW, winH = int(w), int(h)
			persist()
		}
	})

	setStatus("READY. TYPE help FOR DIRECTIVES.")

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.Done():
				return
			case <-t.C:
				s.Update(func() {
					now := time.Now()
					clockLbl.SetText(now.Format("15:04:05"))
					dateLbl.SetText(strings.ToUpper(now.Format("Mon 02 Jan 2006")))
				})
			}
		}
	}()

	rebuildMenu()
	applyCRT()
	start := startDir
	if start == "" {
		start = home
	}
	for _, p := range []string{start, home, "/"} {
		if nav(p) {
			start = p
			break
		}
	}
	expandTo(start)
	tree.SetSelectedKey(start)

	return root
}
