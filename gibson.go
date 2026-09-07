package main

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cfe/sound"

	caution "github.com/nullentropy/caution/go"
)

type vec3 [3]float64

func add(a, b vec3) vec3           { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func sub(a, b vec3) vec3           { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func scale(a vec3, s float64) vec3 { return vec3{a[0] * s, a[1] * s, a[2] * s} }
func dot(a, b vec3) float64        { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross(a, b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func norm(a vec3) vec3 {
	l := math.Sqrt(dot(a, a))
	if l == 0 {
		return a
	}
	return scale(a, 1/l)
}

const (
	kindFile   = 0
	kindDir    = 1
	kindLink   = 2
	kindPortal = 3

	gibCell   = 4.0
	gibFocal  = 1.55
	gibMaxTow = 63
	gibHudTop = 46.0
	gibHudBot = 84.0
	gibSlack  = 24.0
	gibDblMs  = 450 * time.Millisecond

	idleOrbit  = 0
	idleStreet = 1

	entryDefault = 0 // swoop in from a high far vantage
	entryDive    = 1 // emerge low from a jack-in, then rise to survey
	entryUp      = 2 // descend into the parent from directly overhead

	gibMinDist = 3.0
	gibMaxDist = 120.0
	gibMinEl   = 0.06
	gibMaxEl   = 1.45
	gibIdleSec = 4.0
)

type pose struct {
	px, py, pz float64
	yaw, pitch float64
}

type orbit struct {
	tx, ty, tz float64
	dist       float64
	az, el     float64
}

func (o orbit) toPose() pose {
	ce := math.Cos(o.el)
	dir := vec3{ce * math.Sin(o.az), math.Sin(o.el), ce * math.Cos(o.az)}
	return pose{
		px: o.tx + o.dist*dir[0], py: o.ty + o.dist*dir[1], pz: o.tz + o.dist*dir[2],
		yaw: o.az + math.Pi, pitch: -o.el,
	}
}

func (o *orbit) clamp() {
	o.dist = clampF(o.dist, gibMinDist, gibMaxDist)
	o.el = clampF(o.el, gibMinEl, gibMaxEl)
}

func lerpOrbit(a, b orbit, t float64) orbit {
	return orbit{
		tx:   a.tx + (b.tx-a.tx)*t,
		ty:   a.ty + (b.ty-a.ty)*t,
		tz:   a.tz + (b.tz-a.tz)*t,
		dist: a.dist + (b.dist-a.dist)*t,
		az:   a.az + wrapAngle(b.az-a.az)*t,
		el:   a.el + (b.el-a.el)*t,
	}
}

func wrapAngle(d float64) float64 {
	for d > math.Pi {
		d -= 2 * math.Pi
	}
	for d <= -math.Pi {
		d += 2 * math.Pi
	}
	return d
}

type tower struct {
	ent    entry
	name   string
	kind   int
	cx, cz float64
	hw     float64
	hh     float64
}

type gibsonDeps struct {
	s          *caution.Session
	root       *caution.Node
	nav        func(path string) bool
	view       func(e entry, hex bool)
	cur        func() string
	ents       func() []entry
	theme      func() string
	onTarget   func(name string)
	crt        func() (frag string, animate bool, curve float64)
	cmd        func(line string)
	fullscreen func() bool
	soundOn    func() bool
}

type gibson struct {
	gibsonDeps

	overlay *caution.Node
	scene   *caution.Node
	glass   *caution.Node
	topRow  *caution.Node
	nameLbl *caution.Node
	sector  *caution.Node
	counts  *caution.Node
	targetL *caution.Node

	towers []tower
	gridG  int
	total  int

	// orb is the live camera, tgt is where input steers it
	orb, tgt   orbit
	cam        pose
	sel        int
	lastPick   int
	lastPickAt time.Time

	panning, orbiting bool
	dragX, dragY      float64
	dragMoved         bool

	txtWords []uint32
	txtOff   []int
	txtLen   []int

	crtCurve float64

	diving     bool
	tweenGen   int
	enterGen   int
	lastAct    time.Time
	entryStyle int // how the next refresh flies the camera in, so a nav continues the gesture

	cmdField   *caution.Node
	findLbl    *caution.Node
	searchOn   bool
	findQuery  string
	matches    []int
	matchIdx   int
	matchFlags []float64

	idleMode   int
	idleSince  time.Time
	orbitDwell time.Duration
	stAxis     int // 0 = travel along X (fixed Z lane), 1 = along Z (fixed X lane)
	stLane     float64
	stPos      float64
	stDir      float64
	stEnd      float64
	stHalf     float64
	stLegs     int
	stExiting  bool
	stAtExit   bool
	stSpeed    float64
	stAz       float64
	stEl       float64
	stTy       float64
	stDist     float64

	w, h float64
}

func newGibson(d gibsonDeps) *gibson {
	return &gibson{gibsonDeps: d, sel: -1, lastPick: -1, orb: orbit{az: 0.7}}
}

func (g *gibson) isOpen() bool { return g.overlay != nil }

func (g *gibson) toggle() {
	if g.isOpen() {
		g.exit()
	} else {
		g.enter()
	}
}

// chromeAnchor insets the top HUD past the traffic lights under custom chrome,
// unless fullscreen
func (g *gibson) chromeAnchor() caution.A {
	left := 12.0
	if customChrome && (g.fullscreen == nil || !g.fullscreen()) {
		left = 84
	}
	return caution.A{Left: caution.Px(left), Right: caution.Px(12), Top: caution.Px(0), Bottom: caution.Px(0)}
}

func (g *gibson) applyChrome() {
	if g.topRow != nil {
		g.topRow.Anchor(g.chromeAnchor())
	}
}

// sceneDims pins the interactive size from the viewport rather than filling,
// because glass picks are glass-relative
func (g *gibson) sceneDims() (w, h float64) {
	vw, vh := g.s.Viewport()
	if vw < 100 || vh < 100 {
		vw, vh = 1280, 820
	}
	return vw, math.Max(200, vh-gibHudTop-gibHudBot-gibSlack)
}

func (g *gibson) vantage(az float64) orbit {
	return orbit{tx: 0, ty: 2.0, tz: 0, dist: g.orbitR() * 1.7, az: az, el: 0.52}
}

func (g *gibson) farVantage(az float64) orbit {
	return orbit{tx: 0, ty: 2.0, tz: 0, dist: g.orbitR() * 3.1, az: az, el: 0.85}
}

func (g *gibson) enter() {
	if g.isOpen() {
		return
	}
	g.w, g.h = g.sceneDims()
	g.buildTowers()
	g.sel, g.lastPick = -1, -1
	g.lastAct = time.Now()
	g.idleMode = idleOrbit
	g.idleSince = time.Now()
	g.orbitDwell = gibDwell()
	g.orb = g.farVantage(g.orb.az)
	g.tgt = g.orb
	g.cam = g.orb.toPose()

	uni := g.uniforms()
	uni["u_dive"] = 1
	g.scene = caution.Shader(caution.FX{Frag: g.fragSrc(), Animate: true, Uniforms: uni}).
		Frame(0, gibHudTop, g.w, g.h)
	g.glass = caution.Glass().Frame(0, gibHudTop, g.w, g.h)
	g.glass.OnPick(func(x, y float64, _ *caution.Node) { g.onLeftDown(x, y) })
	g.glass.OnDragTo(func(x, y float64) { g.onPan(x, y) })
	g.glass.OnDrop(func(x, y float64) { g.onLeftUp(x, y) })
	g.glass.OnWheel(func(x, y, dx, dy float64) { g.onZoom(x, y, dy) })
	g.glass.OnRightPick(func(x, y float64) { g.onRightDown(x, y) })
	g.glass.OnRightDragTo(func(x, y float64) { g.onOrbit(x, y) })
	g.glass.OnRightDrop(func(x, y float64) { g.orbiting = false })

	g.nameLbl = caution.Label("").Mono().FontSize(11).Weight(700).Color("$accent").
		Frame(-500, -50, 280, 16)
	g.sector = caution.Label("").Mono().FontSize(12).Color("$ink").Fill(1)
	g.counts = caution.Label("").Mono().FontSize(10).Color("$inkFaint")
	g.targetL = caution.Label("NO TARGET").Mono().FontSize(12).Weight(700).Color("$accent")

	g.topRow = caution.HStack().Gap(12).Align("center").Anchor(g.chromeAnchor()).Kids(
		caution.Label("THE GIBSON //").Mono().FontSize(14).Weight(700).Color("$accent"),
		g.sector,
		g.counts,
		caution.Button("UP").OnClick(func() {
			if g.diving {
				return
			}
			g.nav(filepath.Dir(g.cur()))
		}),
		caution.Button("SURFACE").Primary().OnClick(func() { g.exit() }))
	topHud := caution.Panel().Bg("$titlebar").Border("$edge", 1).H(gibHudTop).WindowDrag().
		Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0), Right: caution.Px(0)}).
		Kids(g.topRow)
	g.cmdField = caution.TextField("").Mono().FontSize(12).Fill(1).
		Placeholder("type a name to search · next · prev · clear · cd <dir> · up · surface")
	g.cmdField.OnCommit(func(v string) {
		v = strings.TrimSpace(v)
		g.cmdField.ClearValue()
		if v != "" {
			g.runGibCmd(v)
		}
	})
	g.findLbl = caution.Label("").Mono().FontSize(10).Weight(700).Color("$accent")

	botHud := caution.Panel().Bg("$titlebar").Border("$edge", 1).H(gibHudBot).
		Anchor(caution.A{Left: caution.Px(0), Bottom: caution.Px(0), Right: caution.Px(0)}).
		Kids(caution.VStack().Pad(8).Gap(5).Anchor(caution.A{
			Left: caution.Px(12), Right: caution.Px(12), Top: caution.Px(0), Bottom: caution.Px(0)}).Kids(
			caution.HStack().Gap(8).Align("center").Kids(
				caution.Label("FIND>").Mono().FontSize(12).Weight(700).Color("$accent"),
				g.cmdField,
				g.findLbl),
			caution.HStack().Gap(12).Align("center").Kids(
				g.targetL,
				caution.Panel().Fill(1),
				caution.Label("DRAG PAN · SCROLL ZOOM · RIGHT-DRAG ORBIT · CLICK TARGETS · 2×CLICK JACKS IN").
					Mono().FontSize(9).Color("$inkFaint"))))

	g.overlay = caution.Panel().Bg("$bg").Anchor(fullA(0)).
		Kids(g.scene, g.glass, topHud, botHud, g.nameLbl)
	g.root.Add(g.overlay)
	g.refreshHud()
	g.applyCrt(g.crt())

	g.tweenOrbit(g.vantage(g.orb.az), 1, 0, 900*time.Millisecond, nil)

	if g.soundOn != nil && g.soundOn() {
		g.s.Loop(sound.Ambient())
	}

	g.enterGen++
	gen := g.enterGen
	s := g.s
	go func() {
		tick := time.NewTicker(16 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-s.Done():
				return
			case <-tick.C:
			}
			stop := false
			s.Update(func() {
				if gen != g.enterGen || !g.isOpen() {
					stop = true
					return
				}
				if g.diving {
					return
				}

				if (g.panning || g.orbiting) && time.Since(g.lastAct) > 1200*time.Millisecond {
					g.panning, g.orbiting = false, false
				}
				idle := !g.panning && !g.orbiting && time.Since(g.lastAct).Seconds() > gibIdleSec
				if idle {
					if g.idleMode == idleStreet {
						g.streetStep()
					} else {
						g.orbitDrift()
					}
				} else {
					g.idleMode = idleOrbit
					g.idleSince = time.Now()
				}
				g.tgt.clamp()
				before := g.orb
				g.orb = lerpOrbit(g.orb, g.tgt, 0.22)
				g.orb.clamp()
				moved := math.Abs(g.orb.az-before.az) > 1e-5 ||
					math.Abs(g.orb.el-before.el) > 1e-5 ||
					math.Abs(g.orb.dist-before.dist) > 1e-4 ||
					math.Hypot(g.orb.tx-before.tx, g.orb.tz-before.tz) > 1e-4 ||
					math.Abs(g.orb.ty-before.ty) > 1e-4
				if moved {
					g.cam = g.orb.toPose()
					g.pushCam(0)
					if g.panning || g.orbiting || (idle && g.idleMode == idleStreet) {
						g.hideNameplate()
					} else if g.sel >= 0 {
						g.placeNameplate()
					}
				}
			})
			if stop {
				return
			}
		}
	}()
}

func (g *gibson) camGroundAxes() (rx, rz, fx, fz float64) {
	sa, ca := math.Sin(g.orb.az), math.Cos(g.orb.az)
	fx, fz = -sa, -ca
	rx, rz = -ca, sa
	return
}

func (g *gibson) exit() {
	if !g.isOpen() {
		return
	}
	if g.soundOn != nil && g.soundOn() {
		g.s.Stop(sound.Ambient())
	}
	g.enterGen++
	g.tweenGen++
	g.overlay.Remove()
	g.overlay, g.scene, g.glass, g.nameLbl = nil, nil, nil, nil
	g.diving, g.panning, g.orbiting = false, false, false
}

func (g *gibson) refresh() {
	if !g.isOpen() {
		return
	}
	g.buildTowers()
	g.sel, g.lastPick = -1, -1
	g.scene.SetProp("frag", g.fragSrc())

	az := g.orb.az
	var start orbit
	switch g.entryStyle {
	case entryDive:
		start = orbit{tx: 0, ty: 1.5, tz: 0, dist: g.orbitR() * 0.6, az: az, el: 0.16}
	case entryUp:
		start = orbit{tx: 0, ty: 2.0, tz: 0, dist: g.orbitR() * 1.7, az: az, el: gibMaxEl}
	default:
		start = g.farVantage(az)
	}
	g.entryStyle = entryDefault
	start.clamp()
	g.orb, g.tgt = start, start
	g.cam = g.orb.toPose()
	uni := g.uniforms()
	uni["u_dive"] = 1
	g.scene.SetUniforms(uni)
	g.hideNameplate()
	g.refreshHud()
	g.tweenOrbit(g.vantage(az), 1, 0, 900*time.Millisecond, nil)
}

func (g *gibson) relayout() {
	if !g.isOpen() {
		return
	}
	g.w, g.h = g.sceneDims()
	g.scene.Frame(0, gibHudTop, g.w, g.h)
	g.glass.Frame(0, gibHudTop, g.w, g.h)
	g.placeNameplate()
}

func (g *gibson) applyCrt(frag string, animate bool, curve float64) {
	g.crtCurve = curve
	if g.isOpen() {
		g.overlay.SetProp("effect", map[string]any{"frag": frag, "animate": animate})
	}
}

func (g *gibson) barrelFwd(px, py float64) (float64, float64) {
	if g.crtCurve == 0 {
		return px, py
	}
	cx, cy := px/g.w*2-1, py/g.h*2-1
	k := 1 + g.crtCurve*(cx*cx+cy*cy)
	cx, cy = cx*k, cy*k
	return (cx + 1) * 0.5 * g.w, (cy + 1) * 0.5 * g.h
}

// barrelInv is the first-order inverse of the barrel warp, accurate to O(curve^2)
func (g *gibson) barrelInv(px, py float64) (float64, float64) {
	if g.crtCurve == 0 {
		return px, py
	}
	cx, cy := px/g.w*2-1, py/g.h*2-1
	k := 1 - g.crtCurve*(cx*cx+cy*cy)
	cx, cy = cx*k, cy*k
	return (cx + 1) * 0.5 * g.w, (cy + 1) * 0.5 * g.h
}

func (g *gibson) setTint(theme string) {
	if g.isOpen() {
		g.scene.SetUniforms(accentRGB(theme))
	}
}

func (g *gibson) refreshHud() {
	g.sector.SetText("SECTOR: " + strings.ToUpper(g.cur()))
	n := len(g.towers)
	if g.hasPortal() {
		n--
	}
	c := fmt.Sprintf("%d NODES", n)
	if g.total > n {
		c = fmt.Sprintf("TOP %d OF %d BY MASS", n, g.total)
	}
	g.counts.SetText(c)
	g.targetL.SetText("NO TARGET")
	if g.findLbl != nil {
		g.findLbl.SetText("")
	}
}

func (g *gibson) hasPortal() bool { return g.cur() != "/" }

func (g *gibson) buildTowers() {
	var list []entry
	for _, e := range g.ents() {
		if e.name != ".." {
			list = append(list, e)
		}
	}
	g.total = len(list)

	var dirs, files []entry
	for _, e := range list {
		if e.dir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].size > files[j].size })
	picked := append(append([]entry{}, dirs...), files...)
	if len(picked) > gibMaxTow {
		picked = picked[:gibMaxTow]
	}

	var maxSz int64 = 1
	for _, e := range picked {
		if !e.dir && e.size > maxSz {
			maxSz = e.size
		}
	}

	n := len(picked)
	if g.hasPortal() {
		n++
	}
	G := int(math.Ceil(math.Sqrt(float64(max(n, 1)))))
	g.gridG = max(G, 1)

	slot := func(i int) (cx, cz float64) {
		col, row := i%g.gridG, i/g.gridG
		return (float64(col) + 0.5 - float64(g.gridG)*0.5) * gibCell,
			(float64(row) + 0.5 - float64(g.gridG)*0.5) * gibCell
	}

	portalSlot := -1
	if g.hasPortal() {
		portalSlot = min((g.gridG/2)*g.gridG+g.gridG/2, n-1)
	}
	g.towers = make([]tower, n)
	ei := 0
	for i := 0; i < n; i++ {
		cx, cz := slot(i)
		if i == portalSlot {
			g.towers[i] = tower{name: "..", kind: kindPortal, cx: cx, cz: cz, hw: 0.45, hh: 3.3}
			continue
		}
		e := picked[ei]
		ei++
		t := tower{ent: e, name: e.name, cx: cx, cz: cz}
		switch {
		case e.dir:
			t.kind, t.hw, t.hh = kindDir, 1.15, 2.5
		case e.link:
			t.kind, t.hw, t.hh = kindLink, 0.55, 0.9
		default:
			nrm := math.Log1p(float64(e.size)) / math.Log1p(float64(maxSz))
			t.kind, t.hw, t.hh = kindFile, 0.55+0.5*nrm, 0.45+2.1*nrm
		}
		g.towers[i] = t
	}
	g.txtWords, g.txtOff, g.txtLen = packTowerText(g.cur(), g.towers)

	g.searchOn, g.findQuery, g.matches, g.matchIdx = false, "", nil, 0
	g.matchFlags = make([]float64, len(g.towers))
}

func (g *gibson) orbitR() float64 { return float64(g.gridG)*3.0 + 7.0 }

func (g *gibson) uniforms() map[string]float64 {
	u := accentRGB(g.theme())
	u["u_px"], u["u_py"], u["u_pz"] = g.cam.px, g.cam.py, g.cam.pz
	u["u_yaw"], u["u_pitch"] = g.cam.yaw, g.cam.pitch
	u["u_sel"] = float64(g.sel)
	u["u_dive"] = 0
	if g.searchOn {
		u["u_find"] = 1
	} else {
		u["u_find"] = 0
	}
	return u
}

func (g *gibson) pushCam(fade float64) {
	g.scene.SetUniforms(map[string]float64{
		"u_px": g.cam.px, "u_py": g.cam.py, "u_pz": g.cam.pz,
		"u_yaw": g.cam.yaw, "u_pitch": g.cam.pitch,
		"u_dive": fade,
	})
}

func (g *gibson) tweenOrbit(to orbit, fadeFrom, fadeTo float64, dur time.Duration, then func()) {
	g.tweenGen++
	gen := g.tweenGen
	from := g.orb
	g.diving = true
	s := g.s
	go func() {
		steps := max(1, int(dur/(16*time.Millisecond)))
		for i := 1; i <= steps; i++ {
			select {
			case <-s.Done():
				return
			case <-time.After(16 * time.Millisecond):
			}
			t := float64(i) / float64(steps)
			t = t * t * (3 - 2*t)
			s.Update(func() {
				if gen != g.tweenGen || !g.isOpen() {
					return
				}
				g.orb = lerpOrbit(from, to, t)
				g.cam = g.orb.toPose()
				g.pushCam(fadeFrom + (fadeTo-fadeFrom)*t)
			})
		}
		s.Update(func() {
			if gen != g.tweenGen {
				return
			}
			g.orb, g.tgt = to, to
			g.cam = g.orb.toPose()
			g.diving = false
			g.lastAct = time.Now()
			if then != nil && g.isOpen() {
				then()
			}
		})
	}()
}

func (g *gibson) fragSrc() string {
	n := len(g.towers)
	al := max(n, 1) // pad to >=1: zero-length arrays are illegal GLSL
	var H, W, K, MF strings.Builder
	for i := 0; i < al; i++ {
		if i > 0 {
			H.WriteString(", ")
			W.WriteString(", ")
			K.WriteString(", ")
			MF.WriteString(", ")
		}
		if i < n {
			fmt.Fprintf(&H, "%.4f", g.towers[i].hh)
			fmt.Fprintf(&W, "%.4f", g.towers[i].hw)
			fmt.Fprintf(&K, "%.1f", float64(g.towers[i].kind))
		} else {
			H.WriteString("0.0")
			W.WriteString("0.0")
			K.WriteString("0.0")
		}
		mf := 0.0
		if i < n && i < len(g.matchFlags) {
			mf = g.matchFlags[i]
		}
		fmt.Fprintf(&MF, "%.1f", mf)
	}
	txN, txS := glslUints(g.txtWords)
	toN, toS := glslInts(g.txtOff)
	tlN, tlS := glslInts(g.txtLen)
	ftN, ftS := glslUints(fontBits[:])
	return fmt.Sprintf(`
const int N = %d;
const int G = %d;
const float H[%d] = float[%d](%s);
const float W[%d] = float[%d](%s);
const float K[%d] = float[%d](%s);
const float MF[%d] = float[%d](%s);
const int TO[%d] = int[%d](%s);
const int TL[%d] = int[%d](%s);
const uint TXT[%d] = uint[%d](%s);
const uint FONT[%d] = uint[%d](%s);

float hash21(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123); }

vec2 cellCenter(int i) {
  int col = i - (i / G) * G;
  int row = i / G;
  return (vec2(float(col), float(row)) + 0.5 - float(G) * 0.5) * 4.0;
}

// towerChar is char k of tower i's crawl (0 = space), wrapping over its text
int towerChar(int i, int k) {
  int len = TL[i];
  if (len <= 0) return 0;
  k = k %% len;
  if (k < 0) k += len;
  int b = TO[i] + k;
  return int((TXT[b / 4] >> uint(8 * (b %% 4))) & 255u);
}

// fontPix samples the baked 7x13 font: bit = row*7+col, 3 uints per glyph
bool fontPix(int ch, int px, int py) {
  int bit = py * 7 + px;
  return ((FONT[ch * 3 + bit / 32] >> uint(bit %% 32)) & 1u) != 0u;
}

vec4 shadeTower(int i, vec3 p, vec3 tint) {
  vec2 c = cellCenter(i);
  vec2 l = p.xz - c;
  float hh = H[i];
  float kind = K[i];
  vec3 base = tint;
  if (kind > 2.5)      base = vec3(0.98, 0.98, 0.98);
  else if (kind > 1.5) base = mix(tint, tint.gbr, 0.55);
  else if (kind < 0.5) base = mix(tint, vec3(0.80, 0.80, 0.80), 0.30);
  if (kind < 0.5) base *= 0.78; // files dimmer than dirs: sealed data vs an enterable node

  float matched = (u_find > 0.5 && MF[i] > 0.5) ? 1.0 : 0.0;
  float ghost   = (u_find > 0.5 && MF[i] < 0.5 && kind < 2.5) ? 1.0 : 0.0;
  base *= mix(1.0, 0.12, ghost);
  float mglow = matched * (0.30 + 0.18 * sin(u_time * 3.0));

  float selGlow = 0.0;
  if (float(i) == u_sel) {
    selGlow = 0.45 + 0.25 * sin(u_time * 4.0);
    float spz = fract(u_time * 0.9) * 2.0 * hh;
    selGlow += 0.8 * exp(-10.0 * abs(p.y - spz) / max(hh, 0.5));
  }

  if (p.y >= 2.0 * hh - 0.02) {
    // roofs read the kind from the top-down camera: dir = lit pad with a center
    // cross, file = dim sealed lid, link/portal = plain cap
    vec2 rl = l / max(W[i], 0.001);
    float roofLit = 0.5;
    if (kind > 0.5 && kind < 1.5) {
      float cross = max(smoothstep(0.11, 0.0, abs(rl.x)), smoothstep(0.11, 0.0, abs(rl.y)));
      roofLit = 0.74 + 0.34 * cross;
    } else if (kind < 0.5) {
      roofLit = 0.28;
    }
    return vec4(base * (roofLit + selGlow) + tint * mglow, 0.6 * mix(1.0, 0.5, ghost));
  }

  // fu flips sign per face so the text reads forward on all four sides, not mirrored
  int faceId = (abs(l.x) > abs(l.y)) ? (l.x > 0.0 ? 0 : 1) : (l.y > 0.0 ? 2 : 3);
  float fu = (faceId == 0) ? -l.y : (faceId == 1) ? l.y : (faceId == 2) ? l.x : -l.x;

  float eu = W[i] - abs(fu);
  float ev = min(p.y, 2.0 * hh - p.y);
  float edge = smoothstep(0.10, 0.02, min(eu, ev));

  float sfu = fu + W[i];
  int cols = max(int(floor(2.0 * W[i] / 0.16)), 1);
  int ccol = int(floor(sfu / 0.16));
  float scr = u_time * (0.35 + 0.6 * hash21(vec2(float(i), 51.0)));
  float vv = (2.0 * hh - p.y) / 0.26 + scr;
  int crow = int(floor(vv));
  int ch = towerChar(i, crow * cols + ccol + faceId * 29);
  bool lit = false;
  if (ch > 0 && ch * 3 < FONT.length()) { // 0 = space; icons live past ASCII
    int px = int(fract(sfu / 0.16) * 8.0);
    int py = int(fract(vv) * 14.0);
    lit = px < 7 && py < 13 && fontPix(ch, px, py);
  }

  float tph = hash21(vec2(float(i), 3.7));
  float gate = step(0.72, hash21(vec2(float(i) * 1.3, floor(u_time * 0.25))));
  float pz2 = fract(u_time * (0.25 + 0.35 * tph)) * 2.0 * hh;
  float pulse = gate * exp(-10.0 * abs(p.y - pz2) / max(hh, 0.5));
  if (kind > 2.5) pulse = max(pulse, 0.5 * smoothstep(0.75, 1.0, fract(p.y * 0.7 - u_time * 0.5)));

  vec3 col3 = base * 0.22;
  float a = 0.22;
  col3 = mix(col3, base * 1.30, edge);
  a = max(a, edge * 0.85);
  if (lit) {
    col3 = base * 1.55 + vec3(0.16, 0.16, 0.16) * (1.0 - ghost);
    a = mix(0.92, 0.40, ghost);
  }
  col3 += tint * (selGlow + 0.8 * pulse) + tint * mglow;
  a = min(0.98, a + 0.3 * pulse + 0.3 * selGlow + 0.25 * matched);
  a *= mix(1.0, 0.5, ghost);           // ghosts go see-through so matches show
  return vec4(col3, a);
}

// pcbFloor draws the circuit-board ground. Each trace's presence is hashed from
// the shared edge's MIDPOINT, so both cells agree and traces always connect (no
// floating dashes). fine fades small features toward the horizon to kill moire.
vec3 pcbFloor(vec2 xz, vec3 tint, float fine) {
  vec3 colf = tint * 0.016;
  vec2 p = xz * 1.7;
  vec2 n = floor(p + 0.5);
  vec2 d = p - n;

  float eE = step(0.5, hash21(n + vec2(0.5, 0.0)));
  float eW = step(0.5, hash21(n + vec2(-0.5, 0.0)));
  float eN = step(0.5, hash21(n + vec2(0.0, 0.5)));
  float eS = step(0.5, hash21(n + vec2(0.0, -0.5)));

  float tw = 0.055;
  float hy = smoothstep(tw, tw * 0.35, abs(d.y));
  float hx = smoothstep(tw, tw * 0.35, abs(d.x));
  float tE = eE * hy * step(0.0, d.x);
  float tW = eW * hy * step(0.0, -d.x);
  float tN = eN * hx * step(0.0, d.y);
  float tS = eS * hx * step(0.0, -d.y);
  float trace = max(max(tE, tW), max(tN, tS));
  colf += tint * 0.24 * trace * fine;

  float deg = eE + eW + eN + eS;
  float dn = length(d);
  if (deg > 0.5) {
    float pad = smoothstep(0.19, 0.12, dn) * smoothstep(0.05, 0.10, dn);
    float via = (deg > 2.5 || hash21(n + 3.1) > 0.72) ? smoothstep(0.065, 0.03, dn) : 0.0;
    colf += tint * (0.35 * pad + 0.8 * via) * fine;
  }

  // packet phase comes from global position, so dashes cross nodes without a seam
  float spd = 0.7;
  float pkE = tE * smoothstep(0.86, 0.99, fract(p.x * 0.5 - u_time * spd + hash21(n + 9.0)));
  float pkN = tN * smoothstep(0.86, 0.99, fract(p.y * 0.5 - u_time * spd + hash21(n + 13.0)));
  colf += (tint * 0.6 + vec3(0.4, 0.4, 0.4)) * (pkE + pkN) * fine;

  vec2 qq = xz / 4.0 + float(G) * 0.5;
  vec2 cf = fract(qq);
  vec2 lb = (cf - 0.5) * 4.0;
  float bus = smoothstep(0.09, 0.0, min(2.0 - abs(lb.x), 2.0 - abs(lb.y)));
  colf += tint * 0.30 * bus;
  return colf;
}

vec4 effect(vec2 uv) {
  vec2 sc = uv * 2.0 - 1.0;
  sc.x *= u_res.x / u_res.y;
  sc.y = -sc.y;
  vec3 ro = vec3(u_px, u_py, u_pz);
  float cp = cos(u_pitch);
  vec3 fwd = normalize(vec3(sin(u_yaw) * cp, sin(u_pitch), cos(u_yaw) * cp));
  vec3 rgt = normalize(cross(fwd, vec3(0.0, 1.0, 0.0)));
  vec3 up = cross(rgt, fwd);
  vec3 rd = normalize(fwd * 1.55 + rgt * sc.x + up * sc.y);
  vec3 tint = vec3(u_cr, u_cg, u_cb);
  vec3 tint2 = vec3(u_cr2, u_cg2, u_cb2); // floor tint; == tint unless the theme sets accent2

  vec3 rdn = rd;
  if (abs(rdn.x) < 1e-6) rdn.x = 1e-6;
  if (abs(rdn.y) < 1e-6) rdn.y = 1e-6;
  if (abs(rdn.z) < 1e-6) rdn.z = 1e-6;
  vec3 ird = 1.0 / rdn;

  // track the 4 nearest tower hits (insertion sort) to composite as front-to-back glass
  float t0 = 1e9; float t1w = 1e9; float t2w = 1e9; float t3 = 1e9;
  int i0 = -1; int i1 = -1; int i2 = -1; int i3 = -1;
  for (int i = 0; i < N; i++) {
    if (H[i] <= 0.0) continue;
    vec2 c = cellCenter(i);
    vec3 ta = (vec3(c.x - W[i], 0.0, c.y - W[i]) - ro) * ird;
    vec3 tb = (vec3(c.x + W[i], 2.0 * H[i], c.y + W[i]) - ro) * ird;
    vec3 tn = min(ta, tb);
    vec3 tf = max(ta, tb);
    float tnear = max(max(tn.x, tn.y), tn.z);
    float tfar = min(min(tf.x, tf.y), tf.z);
    if (!(tnear <= tfar && tfar > 0.0 && tnear > 0.01)) continue;
    if (tnear < t0)       { t3 = t2w; i3 = i2; t2w = t1w; i2 = i1; t1w = t0; i1 = i0; t0 = tnear; i0 = i; }
    else if (tnear < t1w) { t3 = t2w; i3 = i2; t2w = t1w; i2 = i1; t1w = tnear; i1 = i; }
    else if (tnear < t2w) { t3 = t2w; i3 = i2; t2w = tnear; i2 = i; }
    else if (tnear < t3)  { t3 = tnear; i3 = i; }
  }
  float tFloor = 1e9;
  if (rd.y < -1e-4) tFloor = -ro.y / rd.y;

  float hT[4] = float[4](t0, t1w, t2w, t3);
  int hI[4] = int[4](i0, i1, i2, i3);
  vec3 accum = vec3(0.0);
  float T = 1.0;
  for (int k = 0; k < 4; k++) {
    if (hI[k] < 0 || hT[k] >= tFloor || T < 0.05) break;
    vec3 p = ro + rd * hT[k];
    vec4 s = shadeTower(hI[k], p, tint);
    float a = s.a * exp(-hT[k] * 0.02);
    accum += T * s.rgb * a;
    T *= 1.0 - a;
  }

  float horizon = pow(max(0.0, 1.0 - abs(rd.y) * 4.0), 3.0);
  vec3 sky = tint * (0.012 + 0.18 * horizon);
  if (T > 0.04) {
    if (tFloor < 1e8) {
      vec3 fp = ro + rd * tFloor;
      float fog = exp(-tFloor * 0.03);
      float fine = clamp(fog * 1.3, 0.0, 1.0);
      accum += T * mix(sky, pcbFloor(fp.xz, tint2, fine), fog);
    } else {
      accum += T * sky;
    }
  }
  vec3 col = accum;
  col = mix(col, vec3(0.0, 0.0, 0.0), clamp(u_dive, 0.0, 1.0));
  return vec4(col, 1.0);
}`,
		n, g.gridG,
		al, al, H.String(), al, al, W.String(), al, al, K.String(), al, al, MF.String(),
		toN, toN, toS, tlN, tlN, tlS, txN, txN, txS, ftN, ftN, ftS)
}

func (g *gibson) camera() (ro, fwd, rgt, up vec3) {
	ro = vec3{g.cam.px, g.cam.py, g.cam.pz}
	cp := math.Cos(g.cam.pitch)
	fwd = norm(vec3{math.Sin(g.cam.yaw) * cp, math.Sin(g.cam.pitch), math.Cos(g.cam.yaw) * cp})
	rgt = norm(cross(fwd, vec3{0, 1, 0}))
	up = cross(rgt, fwd)
	return
}

func (g *gibson) rayAt(px, py float64) (vec3, vec3) {
	scx := (px/g.w*2 - 1) * (g.w / g.h)
	scy := -(py/g.h*2 - 1)
	ro, fwd, rgt, up := g.camera()
	rd := norm(add(add(scale(fwd, gibFocal), scale(rgt, scx)), scale(up, scy)))
	return ro, rd
}

func aabbHit(ro, rd, lo, hi vec3) (float64, bool) {
	tmin, tmax := 0.0, math.Inf(1)
	for i := 0; i < 3; i++ {
		if math.Abs(rd[i]) < 1e-9 {
			if ro[i] < lo[i] || ro[i] > hi[i] {
				return 0, false
			}
			continue
		}
		t1 := (lo[i] - ro[i]) / rd[i]
		t2 := (hi[i] - ro[i]) / rd[i]
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		tmin = math.Max(tmin, t1)
		tmax = math.Min(tmax, t2)
		if tmin > tmax {
			return 0, false
		}
	}
	return tmin, true
}

func (g *gibson) pickTower(px, py float64) int {
	ro, rd := g.rayAt(g.barrelFwd(px, py))
	best, bestT := -1, math.Inf(1)
	for i, t := range g.towers {
		lo := vec3{t.cx - t.hw, 0, t.cz - t.hw}
		hi := vec3{t.cx + t.hw, 2 * t.hh, t.cz + t.hw}
		if tt, ok := aabbHit(ro, rd, lo, hi); ok && tt < bestT {
			best, bestT = i, tt
		}
	}
	return best
}

func (g *gibson) projectTop(t tower) (float64, float64, bool) {
	ro, fwd, rgt, up := g.camera()
	v := sub(vec3{t.cx, 2*t.hh + 0.4, t.cz}, ro)
	z := dot(v, fwd)
	if z < 0.1 {
		return 0, 0, false
	}
	sx := gibFocal * dot(v, rgt) / z
	sy := gibFocal * dot(v, up) / z
	px := (sx/(g.w/g.h) + 1) * g.w / 2
	py := (1 - sy) * g.h / 2
	if px < 0 || px > g.w || py < 0 || py > g.h {
		return 0, 0, false
	}
	return px, py, true
}

// onLeftDown records the press but does not select yet: a press that starts
// moving is a pan, and only a release without movement (onLeftUp) selects
func (g *gibson) onLeftDown(x, y float64) {
	if !g.isOpen() || g.diving {
		return
	}
	g.lastAct = time.Now()
	g.panning = true
	g.dragMoved = false
	g.dragX, g.dragY = x, y
}

func (g *gibson) onPan(x, y float64) {
	if !g.isOpen() || g.diving || !g.panning {
		return
	}
	g.lastAct = time.Now()
	dx := x - g.dragX
	dy := y - g.dragY
	g.dragX, g.dragY = x, y
	if math.Hypot(dx, dy) > 1 {
		g.dragMoved = true
	}
	rx, rz, fx, fz := g.camGroundAxes()
	k := g.tgt.dist * 0.0016
	g.tgt.tx -= (rx*dx + fx*(-dy)) * k
	g.tgt.tz -= (rz*dx + fz*(-dy)) * k
}

func (g *gibson) onLeftUp(x, y float64) {
	g.panning = false
	if !g.isOpen() || g.diving || g.dragMoved {
		return
	}
	idx := g.pickTower(x, y)
	now := time.Now()
	if idx >= 0 && idx == g.lastPick && now.Sub(g.lastPickAt) < gibDblMs {
		g.lastPickAt = time.Time{}
		g.activateTower(idx)
		return
	}
	g.lastPick, g.lastPickAt = idx, now
	g.selectTower(idx)
}

func (g *gibson) onZoom(x, y, dy float64) {
	if !g.isOpen() || g.diving {
		return
	}
	g.lastAct = time.Now()
	g.tgt.dist = clampF(g.tgt.dist*math.Exp(dy*0.0016), gibMinDist, gibMaxDist)
}

func (g *gibson) onRightDown(x, y float64) {
	if !g.isOpen() || g.diving {
		return
	}
	g.lastAct = time.Now()
	g.orbiting = true
	g.dragX, g.dragY = x, y
}

func (g *gibson) onOrbit(x, y float64) {
	if !g.isOpen() || g.diving || !g.orbiting {
		return
	}
	g.lastAct = time.Now()
	dx := x - g.dragX
	dy := y - g.dragY
	g.dragX, g.dragY = x, y
	g.tgt.az -= dx * 0.006
	g.tgt.el = clampF(g.tgt.el+dy*0.005, gibMinEl, gibMaxEl)
}

func (g *gibson) selectTower(idx int) {
	g.sel = idx
	g.scene.SetUniform("u_sel", float64(idx))
	if idx < 0 {
		g.targetL.SetText("NO TARGET")
		g.hideNameplate()
		return
	}
	t := g.towers[idx]
	switch t.kind {
	case kindPortal:
		g.targetL.SetText("TARGET: [UPLINK] " + strings.ToUpper(filepath.Dir(g.cur())))
	case kindDir:
		g.targetL.SetText("TARGET: " + strings.ToUpper(t.name) + " · DIR")
	default:
		g.targetL.SetText(fmt.Sprintf("TARGET: %s · %s · %s",
			strings.ToUpper(t.name), typeCell(t.ent), human(t.ent.size)))
	}
	if t.kind != kindPortal {
		g.onTarget(t.name)
	}
	g.placeNameplate()
}

func (g *gibson) activateTower(idx int) {
	t := g.towers[idx]
	switch t.kind {
	case kindPortal:
		g.diveTo(filepath.Dir(g.cur()), t, true)
	case kindDir:
		g.diveTo(filepath.Join(g.cur(), t.name), t, false)
	default:
		fi := t.ent
		g.view(fi, false)
	}
}

func (g *gibson) diveTo(path string, t tower, up bool) {
	g.hideNameplate()
	target := orbit{
		tx: t.cx, ty: t.hh, tz: t.cz,
		dist: t.hw + 1.4, az: g.orb.az, el: 0.12,
	}
	if up {
		// the uplink rises up and out of the sector on the way to the parent
		target = orbit{tx: t.cx, ty: t.hh * 2, tz: t.cz, dist: g.orbitR() * 1.5, az: g.orb.az, el: gibMaxEl}
	}
	g.tweenOrbit(target, 0, 1, 650*time.Millisecond, func() {
		g.entryStyle = entryDive
		if up {
			g.entryStyle = entryUp
		}
		if !g.nav(path) {
			g.entryStyle = entryDefault
			g.tweenOrbit(g.vantage(g.orb.az), 1, 0, 500*time.Millisecond, nil)
		}
	})
}

func (g *gibson) placeNameplate() {
	if !g.isOpen() || g.sel < 0 || g.sel >= len(g.towers) {
		return
	}
	t := g.towers[g.sel]
	px, py, ok := g.projectTop(t)
	if !ok {
		g.hideNameplate()
		return
	}
	px, py = g.barrelInv(px, py)
	label := "» " + t.name
	if t.kind == kindPortal {
		label = "» UPLINK"
	}
	g.nameLbl.SetText(label)
	g.nameLbl.Frame(clampF(px+8, 0, g.w-160), clampF(py-20+gibHudTop, gibHudTop, gibHudTop+g.h-16), 280, 16)
}

func (g *gibson) hideNameplate() {
	if g.nameLbl != nil {
		g.nameLbl.Frame(-500, -50, 280, 16)
	}
}

func (g *gibson) focusCmd() {
	if g.isOpen() && g.cmdField != nil {
		g.cmdField.Focus()
	}
}

func (g *gibson) runGibCmd(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if strings.HasPrefix(line, "/") {
		g.runFind(strings.TrimSpace(line[1:]))
		return
	}
	fields := strings.Fields(line)
	verb := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
	switch verb {
	case "find", "f", "grep", "search":
		g.runFind(arg)
	case "next", "n":
		g.cycleMatch(1)
	case "prev", "p":
		g.cycleMatch(-1)
	case "clear", "unfind":
		g.clearFind()
		g.selectTower(-1)
	case "up", "..":
		if !g.diving {
			g.nav(filepath.Dir(g.cur()))
		}
	case "surface", "exit", "quit":
		g.exit()
	case "cd", "home", "root", "help", "?", "crt", "hidden", "dotfiles", "pwd", "ls", "refresh", "rescan":
		if g.cmd != nil {
			g.cmd(line)
		}
	default:
		g.runFind(line)
	}
}

func (g *gibson) runFind(q string) {
	if !g.isOpen() {
		return
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		g.clearFind()
		return
	}
	g.matchFlags = make([]float64, len(g.towers))
	g.matches = g.matches[:0]
	for i, t := range g.towers {
		if t.kind == kindPortal {
			continue
		}
		if strings.Contains(strings.ToLower(t.name), q) {
			g.matches = append(g.matches, i)
			g.matchFlags[i] = 1
		}
	}
	g.findQuery = q
	if len(g.matches) == 0 {
		g.searchOn = false
		g.scene.SetUniform("u_find", 0)
		g.findLbl.SetText("NO MATCH: " + strings.ToUpper(q))
		return
	}
	g.searchOn = true
	g.matchIdx = 0
	g.scene.SetProp("frag", g.fragSrc())
	g.scene.SetUniform("u_find", 1)
	g.updateFindLbl()
	g.selectTower(g.matches[0])
	g.hideNameplate()
	g.frameTower(g.matches[0])
}

func (g *gibson) cycleMatch(delta int) {
	if !g.searchOn || len(g.matches) == 0 {
		g.findLbl.SetText("NO ACTIVE SEARCH")
		return
	}
	g.matchIdx = (g.matchIdx + delta + len(g.matches)) % len(g.matches)
	g.updateFindLbl()
	idx := g.matches[g.matchIdx]
	g.selectTower(idx)
	g.hideNameplate()
	g.frameTower(idx)
}

func (g *gibson) clearFind() {
	g.searchOn, g.findQuery, g.matches, g.matchIdx = false, "", nil, 0
	if g.scene != nil {
		g.scene.SetUniform("u_find", 0)
	}
	if g.findLbl != nil {
		g.findLbl.SetText("")
	}
}

func (g *gibson) updateFindLbl() {
	g.findLbl.SetText(fmt.Sprintf("MATCH %d/%d: %s",
		g.matchIdx+1, len(g.matches), strings.ToUpper(g.findQuery)))
}

func (g *gibson) frameTower(idx int) {
	if idx < 0 || idx >= len(g.towers) {
		return
	}
	t := g.towers[idx]
	to := orbit{
		tx: t.cx, ty: t.hh, tz: t.cz,
		dist: clampF(t.hw*3.0+9.0, gibMinDist, gibMaxDist),
		az:   g.orb.az, el: 0.5,
	}
	g.tweenOrbit(to, 0, 0, 700*time.Millisecond, func() {
		if g.sel >= 0 {
			g.placeNameplate()
		}
	})
}

func gibDwell() time.Duration {
	return time.Duration(5000+rand.Intn(4500)) * time.Millisecond
}

func (g *gibson) orbitDrift() {
	g.tgt.az += 0.0016
	home := g.vantage(g.tgt.az)
	g.tgt.tx += (home.tx - g.tgt.tx) * 0.01
	g.tgt.ty += (home.ty - g.tgt.ty) * 0.01
	g.tgt.tz += (home.tz - g.tgt.tz) * 0.01
	g.tgt.dist += (home.dist - g.tgt.dist) * 0.01
	g.tgt.el += (home.el - g.tgt.el) * 0.01
	if g.gridG >= 3 && time.Since(g.idleSince) >= g.orbitDwell {
		if rand.Float64() < 0.72 {
			g.beginStreetRun()
		} else {
			g.idleSince = time.Now()
			g.orbitDwell = gibDwell()
		}
	}
}

func (g *gibson) beginStreetRun() {
	G := g.gridG
	if G < 3 {
		g.idleSince = time.Now()
		g.orbitDwell = gibDwell()
		return
	}
	g.idleMode = idleStreet
	g.idleSince = time.Now()
	g.stHalf = float64(G)*gibCell*0.5 + gibCell*1.3
	g.stLegs = 2 + rand.Intn(2)
	g.stExiting = false
	secs := 9.0 + rand.Float64()*4.0
	g.stSpeed = (2.0 * g.stHalf) / (secs * 60.0)
	g.stEl, g.stTy = 0.085, 1.35
	g.stDist = clampF(gibCell*2.0, 6.5, 12.0)
	lanes := g.lanes()
	axis := rand.Intn(2)
	lane := lanes[rand.Intn(len(lanes))]
	x, z := g.tgt.tx, g.tgt.tz
	if axis == 1 {
		x = lane
	} else {
		z = lane
	}
	along := z
	if axis == 0 {
		along = x
	}
	dir, ok := g.pickDir(along)
	if !ok {
		dir = 1
	}
	g.startLeg(x, z, axis, dir)
}

func (g *gibson) streetAz() float64 {
	if g.stAxis == 1 {
		if g.stDir > 0 {
			return math.Pi
		}
		return 0
	}
	if g.stDir > 0 {
		return -math.Pi / 2
	}
	return math.Pi / 2
}

func (g *gibson) lanes() []float64 {
	G := g.gridG
	out := make([]float64, 0, G-1)
	for j := 1; j < G; j++ {
		out = append(out, (float64(j)-float64(G)*0.5)*gibCell)
	}
	return out
}

// laneAhead picks the FARTHEST interior cross-street ahead, so a leg is a long
// cruise instead of a quick hop to the next gap
func (g *gibson) laneAhead(pos, dir float64) (end float64, ok bool) {
	lead := gibCell * 0.5
	best, found := 0.0, false
	for _, lc := range g.lanes() {
		d := (lc - pos) * dir
		if d >= lead && (!found || d > (best-pos)*dir) {
			best, found = lc, true
		}
	}
	if !found {
		return g.stHalf * dir, false
	}
	return best, true
}

// pickDir chooses a direction that still has a cross-street ahead, so a turn
// keeps the tour among the towers instead of out over the empty grid
func (g *gibson) pickDir(pos float64) (float64, bool) {
	var ok []float64
	for _, d := range []float64{1, -1} {
		if _, has := g.laneAhead(pos, d); has {
			ok = append(ok, d)
		}
	}
	if len(ok) == 0 {
		return 0, false
	}
	return ok[rand.Intn(len(ok))], true
}

func (g *gibson) streetPoint() (x, z float64) {
	if g.stAxis == 1 {
		return g.stLane, g.stPos
	}
	return g.stPos, g.stLane
}

func (g *gibson) startLeg(x, z float64, axis int, dir float64) {
	g.stAxis, g.stDir = axis, dir
	if axis == 1 {
		g.stLane, g.stPos = x, z
	} else {
		g.stLane, g.stPos = z, x
	}
	end, ok := g.laneAhead(g.stPos, dir)
	g.stEnd, g.stAtExit = end, !ok
	g.stAz = g.streetAz()
}

// beginStreetExit ends the tour with a forward climb
func (g *gibson) beginStreetExit() {
	g.stExiting = true
	g.stEnd = g.stHalf * g.stDir
	g.stEl, g.stTy = 0.52, 2.0
	g.stDist = g.orbitR() * 1.1
}

func (g *gibson) streetStep() {
	g.stPos += g.stSpeed * g.stDir
	if g.stAxis == 1 {
		g.tgt.tx += (g.stLane - g.tgt.tx) * 0.05
		g.tgt.tz += (g.stPos - g.tgt.tz) * 0.20
	} else {
		g.tgt.tz += (g.stLane - g.tgt.tz) * 0.05
		g.tgt.tx += (g.stPos - g.tgt.tx) * 0.20
	}
	g.tgt.ty += (g.stTy - g.tgt.ty) * 0.05
	g.tgt.dist += (g.stDist - g.tgt.dist) * 0.05
	g.tgt.el += (g.stEl - g.tgt.el) * 0.05
	g.tgt.az += wrapAngle(g.stAz-g.tgt.az) * 0.05

	if g.stExiting {
		if g.tgt.el > 0.47 {
			g.idleMode = idleOrbit
			g.idleSince = time.Now()
			g.orbitDwell = gibDwell()
		}
		return
	}
	if (g.stPos-g.stEnd)*g.stDir < 0 {
		return
	}
	x, z := g.streetPoint()
	axis := 1 - g.stAxis
	along := z
	if axis == 0 {
		along = x
	}
	dir, ok := g.pickDir(along)
	if g.stLegs <= 1 || g.stAtExit || !ok {
		g.beginStreetExit()
		return
	}
	g.stLegs--
	g.startLeg(x, z, axis, dir)
}

func clampF(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
