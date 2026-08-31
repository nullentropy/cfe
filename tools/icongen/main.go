package main

import (
	"image"
	"image/png"
	"log"
	"math"
	"os"
)

const size = 1024

type rgb [3]float64

func lerp(a, b rgb, t float64) rgb {
	return rgb{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

func roundRectDist(px, py, x0, y0, x1, y1, r float64) float64 {
	hx, hy := (x1-x0)/2, (y1-y0)/2
	mx, my := (x0+x1)/2, (y0+y1)/2
	dx := math.Abs(px-mx) - hx + r
	dy := math.Abs(py-my) - hy + r
	outside := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
	inside := math.Min(math.Max(dx, dy), 0)
	return outside + inside - r
}

func distSeg(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	wx, wy := px-ax, py-ay
	t := (wx*vx + wy*vy) / (vx*vx + vy*vy)
	t = math.Max(0, math.Min(1, t))
	cx, cy := ax+t*vx, ay+t*vy
	return math.Hypot(px-cx, py-cy)
}

func main() {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	const (
		cx, cy     = size / 2.0, size / 2.0
		tileInset  = 100.0
		tileRadius = 180.0
		ringInset  = 34.0
		ringThick  = 10.0
	)
	bg := rgb{0x02, 0x06, 0x04}
	ring := rgb{0x1f, 0x6b, 0x3e}
	phosphor := rgb{0x00, 0xff, 0x66}

	top := [2]float64{312, 332}
	vert := [2]float64{532, 512}
	bot := [2]float64{312, 692}
	const chevThick = 76.0
	curL, curT, curR, curB := 572.0, 362.0, 712.0, 662.0
	const curRadius = 22.0

	ringOuterR := tileRadius - ringInset

	for y := range size {
		for x := range size {
			fx, fy := float64(x)+0.5, float64(y)+0.5

			td := roundRectDist(fx, fy, tileInset, tileInset, size-tileInset, size-tileInset, tileRadius)
			if td > 0.5 {
				continue
			}
			tileA := 1.0
			if td > -0.5 {
				tileA = 0.5 - td
			}

			c := bg

			rd := roundRectDist(fx, fy, tileInset+ringInset, tileInset+ringInset,
				size-tileInset-ringInset, size-tileInset-ringInset, ringOuterR)
			if ringA := math.Max(0, 1-math.Abs(rd)/(ringThick/2)); ringA > 0 {
				c = lerp(c, ring, ringA)
			}

			d1 := distSeg(fx, fy, top[0], top[1], vert[0], vert[1]) - chevThick/2
			d2 := distSeg(fx, fy, vert[0], vert[1], bot[0], bot[1]) - chevThick/2
			d3 := roundRectDist(fx, fy, curL, curT, curR, curB, curRadius)
			gd := math.Min(math.Min(d1, d2), d3)

			if gd < 70 {
				glowA := math.Max(0, 1-gd/70) * 0.5
				c[0] = math.Min(255, c[0]+phosphor[0]*glowA)
				c[1] = math.Min(255, c[1]+phosphor[1]*glowA)
				c[2] = math.Min(255, c[2]+phosphor[2]*glowA)
			}

			if gd < 0.5 {
				fillA := 1.0
				if gd > -0.5 {
					fillA = 0.5 - gd
				}
				c = lerp(c, phosphor, fillA)
			}

			o := img.PixOffset(x, y)
			img.Pix[o] = uint8(c[0] * tileA)
			img.Pix[o+1] = uint8(c[1] * tileA)
			img.Pix[o+2] = uint8(c[2] * tileA)
			img.Pix[o+3] = uint8(255 * tileA)
		}
	}

	f, err := os.Create("icon.png")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote icon.png")
}
