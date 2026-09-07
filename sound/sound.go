package sound

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
)

const sr = 48000

const loopSecs = 8.0

type bank struct {
	wav map[string][]byte
	uri map[string]string
}

var built = sync.OnceValue(build)

func build() *bank {
	wav := map[string][]byte{
		"ambient": encodeWAV(genAmbient()),
		"select":  encodeWAV(genBlip([]float64{659.25, 987.75}, 0.045, 2, 0.42)),
		"press":   encodeWAV(genBlip([]float64{440}, 0.06, 3, 0.46)),
		"tick":    encodeWAV(genTick()),
		"open":    encodeWAV(genBlip([]float64{523.25, 784}, 0.06, 2, 0.4)),
		"close":   encodeWAV(genBlip([]float64{784, 523.25}, 0.06, 2, 0.4)),
		"type":    encodeWAV(genTap(180, 0.09)),
	}
	uri := make(map[string]string, len(wav))
	for k, v := range wav {
		uri[k] = "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(v)
	}
	return &bank{wav: wav, uri: uri}
}

func (b *bank) at(name string) string { return b.uri[name] }

func Warm() { built() }

func writeFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

func Ambient() string { return built().at("ambient") }

func Gestures() map[string]string {
	b := built()
	return map[string]string{
		"press":  b.at("press"),
		"select": b.at("select"),
		"toggle": b.at("tick"),
		"open":   b.at("open"),
		"close":  b.at("close"),
		"type":   b.at("type"),
	}
}

func Preload() []string {
	b := built()
	return []string{
		b.at("ambient"),
		b.at("select"), b.at("press"), b.at("tick"), b.at("open"),
		b.at("close"), b.at("type"),
	}
}

func Dump(dir string) error {
	for name, data := range built().wav {
		if err := writeFile(dir, name+".wav", data); err != nil {
			return err
		}
	}
	return nil
}

func genAmbient() []float32 {
	r := rand.New(rand.NewSource(20240611))
	n := int(loopSecs * sr)
	out := make([]float64, n)

	for i := range out {
		t := float64(i) / sr
		out[i] += 0.16 * (math.Sin(2*math.Pi*55.0*t) + math.Sin(2*math.Pi*55.125*t)) * 0.5
	}

	chords := [][]float64{
		{220.00, 261.63, 329.63}, // Am
		{174.61, 220.00, 261.63}, // F
		{261.63, 329.63, 392.00}, // C
		{196.00, 246.94, 293.66}, // G
	}
	barN := n / 4
	fade := int(0.3 * sr)
	for b, triad := range chords {
		for _, f := range triad {
			ph := r.Float64() * 2 * math.Pi
			for _, detune := range []float64{-0.15, 0.15} {
				for k := 0; k < barN; k++ {
					i := b*barN + k
					t := float64(k) / sr
					out[i] += 0.05 * rcWin(k, barN, fade) * math.Sin(2*math.Pi*(f+detune)*t+ph)
				}
			}
		}
	}

	beatN := n / 16
	bass := []float64{110.0, 87.31, 130.81, 98.00}
	for beat := 0; beat < 16; beat++ {
		f := bass[beat/4]
		for k := 0; k < beatN; k++ {
			i := beat*beatN + k
			t := float64(k) / sr
			e := math.Exp(-t/0.18) * ramp(k, int(0.004*sr))
			var s float64
			for h := 1; h <= 5; h++ {
				s += math.Sin(2*math.Pi*f*float64(h)*t) / float64(h)
			}
			out[i] += 0.10 * e * s
		}
	}

	for beat := 0; beat < 16; beat++ {
		start := beat*beatN + beatN/2
		for k := 0; k < int(0.05*sr) && start+k < n; k++ {
			out[start+k] += 0.05 * math.Exp(-float64(k)/sr/0.02) * (r.Float64()*2 - 1)
		}
	}

	for i := range out {
		t := float64(i) / sr
		breath := 0.5 - 0.5*math.Cos(2*math.Pi*t/loopSecs)
		out[i] += 0.03 * breath * (math.Sin(2*math.Pi*440.0*t) + math.Sin(2*math.Pi*659.25*t))
	}

	seam := int(0.003 * sr)
	for k := 0; k < seam; k++ {
		w := 0.5 - 0.5*math.Cos(math.Pi*float64(k)/float64(seam))
		out[k] *= w
		out[n-1-k] *= w
	}

	normalize(out, 0.5)
	return f32(out)
}

func genBlip(freqs []float64, seg float64, harm int, level float64) []float32 {
	segN := int(seg * sr)
	out := make([]float64, segN*len(freqs))
	for j, f := range freqs {
		for k := 0; k < segN; k++ {
			t := float64(k) / sr
			e := math.Exp(-t/(seg*0.5)) * ramp(k, int(0.003*sr))
			var s float64
			for h := 1; h <= harm; h++ {
				s += math.Sin(2*math.Pi*f*float64(h)*t) / float64(h)
			}
			out[j*segN+k] = e * s
		}
	}
	normalize(out, level)
	return f32(out)
}

func genTick() []float32 {
	r := rand.New(rand.NewSource(7))
	n := int(0.03 * sr)
	out := make([]float64, n)
	for k := 0; k < n; k++ {
		t := float64(k) / sr
		e := math.Exp(-t / 0.008)
		out[k] = e * (0.7*math.Sin(2*math.Pi*1318.5*t) + 0.3*(r.Float64()*2-1))
	}
	normalize(out, 0.32)
	return f32(out)
}

func genTap(cutoff, level float64) []float32 {
	r := rand.New(rand.NewSource(11))
	n := int(0.022 * sr)
	out := make([]float64, n)
	a := 1 - math.Exp(-2*math.Pi*cutoff/sr)
	var lp float64
	for k := 0; k < n; k++ {
		t := float64(k) / sr
		lp += a * ((r.Float64()*2 - 1) - lp)
		body := math.Sin(2*math.Pi*140*t) * math.Exp(-t/0.006)
		out[k] = (0.8*lp + 0.2*body) * math.Exp(-t/0.005)
	}
	normalize(out, level)
	return f32(out)
}

func rcWin(k, n, fade int) float64 {
	if fade*2 > n {
		fade = n / 2
	}
	switch {
	case k < fade:
		return 0.5 - 0.5*math.Cos(math.Pi*float64(k)/float64(fade))
	case k >= n-fade:
		return 0.5 - 0.5*math.Cos(math.Pi*float64(n-1-k)/float64(fade))
	default:
		return 1
	}
}

func ramp(k, a int) float64 {
	if a > 0 && k < a {
		return float64(k) / float64(a)
	}
	return 1
}

func normalize(x []float64, peak float64) {
	m := 0.0
	for _, v := range x {
		if a := math.Abs(v); a > m {
			m = a
		}
	}
	if m == 0 {
		return
	}
	g := peak / m
	for i := range x {
		x[i] *= g
	}
}

func f32(x []float64) []float32 {
	o := make([]float32, len(x))
	for i, v := range x {
		o[i] = float32(v)
	}
	return o
}

func encodeWAV(mono []float32) []byte {
	const hdr = 44
	data := hdr + len(mono)*2
	b := make([]byte, data)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(data-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], 1) // mono
	binary.LittleEndian.PutUint32(b[24:], sr)
	binary.LittleEndian.PutUint32(b[28:], sr*2) // byte rate
	binary.LittleEndian.PutUint16(b[32:], 2)    // block align
	binary.LittleEndian.PutUint16(b[34:], 16)   // bits
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(mono)*2))
	for i, f := range mono {
		v := math.Round(float64(f) * 32767)
		binary.LittleEndian.PutUint16(b[hdr+i*2:], uint16(int16(math.Max(-32768, math.Min(32767, v)))))
	}
	return b
}
