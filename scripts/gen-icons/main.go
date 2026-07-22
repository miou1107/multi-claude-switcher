//go:build ignore

// gen-icons rasterizes the Multi-Claude Switcher "eyes" mark into every asset
// the project ships, straight from geometry — no external SVG/rasterizer tools
// needed. Run from the repo root:
//
//	go run scripts/gen-icons/main.go
//
// Outputs:
//
//	cmd/mcs-tray/assets/appicon-1024.png  color app-icon source (macOS .icns is
//	                                       generated from this at packaging time)
//	cmd/mcs-tray/assets/icon.png          black menu-bar template (macOS recolors)
//	cmd/mcs-tray/assets/icon.ico          color multi-resolution Windows icon
//	docs/assets/icon.png                  512px color icon for README / docs
//
// The mark: a pair of eyes, left large, right small, each with a pupil. Colors
// and geometry below are the single source of truth — edit here and re-run.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// Supersampling factor per axis (SS*SS subsamples per output pixel) for
// anti-aliasing. 4 is plenty for these smooth shapes.
const SS = 4

// ---- palette --------------------------------------------------------------

type rgb struct{ r, g, b float64 } // 0..1

var (
	clayTL = hex(0xe8, 0x8b, 0x6a) // gradient top-left
	clayBR = hex(0xc1, 0x5f, 0x3c) // gradient bottom-right
	white  = hex(0xff, 0xff, 0xff)
	pupil  = hex(0x3a, 0x1f, 0x14) // dark warm brown
)

func hex(r, g, b uint8) rgb {
	return rgb{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

// ---- geometry (all in a 0..100 canvas) ------------------------------------

const (
	bgMargin = 6.0  // transparent border around the rounded tile
	bgRadius = 22.0 // corner radius of the tile
)

type circle struct{ cx, cy, r float64 }

var (
	eyeL   = circle{37, 50, 21}  // left eye (large)
	eyeR   = circle{74, 53, 12}  // right eye (small)
	pupL   = circle{37, 50, 8.5} // left pupil
	pupR   = circle{74, 53, 5}   // right pupil
	catchL = circle{40.5, 46.5, 2.6}
	catchR = circle{76, 50.5, 1.5}
)

func (c circle) inside(x, y float64) bool {
	return math.Hypot(x-c.cx, y-c.cy) <= c.r
}

// roundedRectInside reports whether (x,y) lies within the tile [m,100-m]
// with corner radius r.
func roundedRectInside(x, y, m, r float64) bool {
	hx, hy := (100-2*m)/2, (100-2*m)/2
	cx, cy := 50.0, 50.0
	px, py := math.Abs(x-cx)-(hx-r), math.Abs(y-cy)-(hy-r)
	qx, qy := math.Max(px, 0), math.Max(py, 0)
	d := math.Hypot(qx, qy) + math.Min(math.Max(px, py), 0) - r
	return d <= 0
}

// ---- samplers: given a point in 0..100, return straight (un-premultiplied)
// RGBA in 0..1 ----------------------------------------------------------------

// colorIcon: clay tile + white eyes + dark pupils + catchlights.
func colorIcon(x, y float64) (rgb, float64) {
	if catchL.inside(x, y) || catchR.inside(x, y) {
		return white, 1
	}
	if pupL.inside(x, y) || pupR.inside(x, y) {
		return pupil, 1
	}
	if eyeL.inside(x, y) || eyeR.inside(x, y) {
		return white, 1
	}
	if roundedRectInside(x, y, bgMargin, bgRadius) {
		// diagonal gradient across the tile
		span := 100 - 2*bgMargin
		t := 0.5 * (((x - bgMargin) / span) + ((y - bgMargin) / span))
		t = math.Max(0, math.Min(1, t))
		return rgb{
			clayTL.r + (clayBR.r-clayTL.r)*t,
			clayTL.g + (clayBR.g-clayTL.g)*t,
			clayTL.b + (clayBR.b-clayTL.b)*t,
		}, 1
	}
	return rgb{}, 0
}

// templateMark: black eye donuts (outer eye disc minus pupil hole), transparent
// everywhere else. macOS tints this to match the menu bar.
func templateMark(x, y float64) (rgb, float64) {
	inL := eyeL.inside(x, y) && !pupL.inside(x, y)
	inR := eyeR.inside(x, y) && !pupR.inside(x, y)
	if inL || inR {
		return rgb{0, 0, 0}, 1
	}
	return rgb{}, 0
}

// ---- rendering ------------------------------------------------------------

// renderSquare renders a size×size image where the 0..100 canvas maps to the
// whole image.
func renderSquare(size int, sample func(x, y float64) (rgb, float64)) *image.NRGBA {
	return render(size, size, func(px, py float64) (rgb, float64) {
		return sample(px/float64(size)*100, py/float64(size)*100)
	})
}

// renderTemplate renders the eyes tightly cropped to the given pixel box, so
// the mark fills the menu-bar height instead of floating in padding.
func renderTemplate(w, h int) *image.NRGBA {
	// padded bounding box of the eye group, in 0..100 space
	const x0, x1 = 12.0, 90.0
	const y0, y1 = 25.0, 75.0
	return render(w, h, func(px, py float64) (rgb, float64) {
		x := x0 + px/float64(w)*(x1-x0)
		y := y0 + py/float64(h)*(y1-y0)
		return templateMark(x, y)
	})
}

// render maps output pixel (px+0.5,py+0.5) via mapper and supersamples.
func render(w, h int, mapper func(px, py float64) (rgb, float64)) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			var sr, sg, sb, sa float64
			for sy := 0; sy < SS; sy++ {
				for sx := 0; sx < SS; sx++ {
					fx := float64(px) + (float64(sx)+0.5)/SS
					fy := float64(py) + (float64(sy)+0.5)/SS
					c, a := mapper(fx, fy)
					sr += c.r * a
					sg += c.g * a
					sb += c.b * a
					sa += a
				}
			}
			n := float64(SS * SS)
			a := sa / n
			var r, g, b float64
			if sa > 0 {
				r, g, b = sr/sa, sg/sa, sb/sa
			}
			img.Pix[py*img.Stride+px*4+0] = to8(r)
			img.Pix[py*img.Stride+px*4+1] = to8(g)
			img.Pix[py*img.Stride+px*4+2] = to8(b)
			img.Pix[py*img.Stride+px*4+3] = to8(a)
		}
	}
	return img
}

func to8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

// ---- output ---------------------------------------------------------------

func writePNG(path string, img image.Image) {
	must(os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	must(err)
	defer f.Close()
	must(png.Encode(f, img))
	fmt.Printf("  wrote %s (%dx%d)\n", path, img.Bounds().Dx(), img.Bounds().Dy())
}

// writeICO packs color PNG images (one per size) into a Windows .ico. PNG-in-ICO
// is supported on Windows Vista and later.
func writeICO(path string, sizes []int) {
	type entry struct {
		size int
		data []byte
	}
	var entries []entry
	for _, s := range sizes {
		var buf bytes.Buffer
		must(png.Encode(&buf, renderSquare(s, colorIcon)))
		entries = append(entries, entry{s, buf.Bytes()})
	}

	var out bytes.Buffer
	// ICONDIR
	binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := byte(e.size)                                          // 256 wraps to 0, which ICO uses to mean 256
		out.WriteByte(dim)                                           // width
		out.WriteByte(dim)                                           // height
		out.WriteByte(0)                                             // color count
		out.WriteByte(0)                                             // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1))           // planes
		binary.Write(&out, binary.LittleEndian, uint16(32))          // bpp
		binary.Write(&out, binary.LittleEndian, uint32(len(e.data))) // size
		binary.Write(&out, binary.LittleEndian, uint32(offset))      // offset
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	must(os.MkdirAll(filepath.Dir(path), 0o755))
	must(os.WriteFile(path, out.Bytes(), 0o644))
	fmt.Printf("  wrote %s (%d sizes: %v)\n", path, len(sizes), sizes)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	p := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }

	fmt.Println("Generating icons:")
	writePNG(p("cmd", "mcs-tray", "assets", "appicon-1024.png"), renderSquare(1024, colorIcon))
	writePNG(p("cmd", "mcs-tray", "assets", "icon.png"), renderTemplate(69, 44))
	writeICO(p("cmd", "mcs-tray", "assets", "icon.ico"), []int{16, 32, 48, 64, 128, 256})
	writePNG(p("docs", "assets", "icon.png"), renderSquare(512, colorIcon))
	fmt.Println("Done.")
}
