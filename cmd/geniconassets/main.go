// geniconassets regenerates build/appicon.png and build/windows/icon.ico - a
// flat printer glyph on a rounded-square badge, in the app's own accent
// color (matching the "Deploy Checked Printers" button). Exists as a repo
// utility (like cmd/pdtdebug) rather than a one-off script since there's no
// image-editing tool in this environment to hand-author/tweak an icon with;
// re-run it (`go run ./cmd/geniconassets`) after changing the colors/geometry
// below rather than editing the generated files directly.
//
// Go's standard library has no ICO encoder, so writeICO below builds one
// directly: modern Windows (Vista+) accepts PNG-compressed frames in an ICO
// container for any size, which is far simpler than the classic BMP+AND-mask
// format ICO otherwise requires.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
)

// badgeColor matches app.css's button.primary ("Deploy Checked Printers").
var (
	badgeColor  = color.RGBA{0x2f, 0x6f, 0x4f, 0xff}
	glyphColor  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	accentColor = color.RGBA{0x1a, 0x25, 0x36, 0xff} // matches app.css's dark navy panel background
)

func main() {
	icoSizes := []int{16, 24, 32, 48, 64, 128, 256}
	images := make([]image.Image, len(icoSizes))
	for i, size := range icoSizes {
		images[i] = drawIcon(size)
	}
	mustWriteFile("build/windows/icon.ico", func(f *os.File) error { return writeICO(f, images) })

	appIcon := drawIcon(1024)
	mustWriteFile("build/appicon.png", func(f *os.File) error { return png.Encode(f, appIcon) })
}

func mustWriteFile(path string, write func(*os.File) error) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := write(f); err != nil {
		panic(err)
	}
}

// supersample is the internal-render-to-output ratio: every icon is drawn
// at size*supersample with hard pixel edges, then box-filtered down to
// size - since Go's image package has no anti-aliased drawing primitives,
// this is what keeps the badge's rounded corners and the gaps between the
// paper/body/paper bands smooth rather than jagged, especially at the
// smallest ICO sizes (16px, 24px) where a single un-anti-aliased pixel row
// is the difference between a visible gap and none at all.
const supersample = 4

// drawIcon renders the printer glyph at size x size.
func drawIcon(size int) image.Image {
	return downsample(drawIconAt(size*supersample), size)
}

func drawIconAt(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	radius := size * 18 / 100
	inBadge := roundedRectMask(size, radius)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if inBadge(x, y) {
				img.Set(x, y, badgeColor)
			}
		}
	}

	frac := func(n int) int { return n * size / 100 }
	fillRect(img, frac(32), frac(14), frac(68), frac(34), glyphColor)  // paper feeding in
	fillRect(img, frac(20), frac(36), frac(80), frac(64), glyphColor)  // printer body
	fillRect(img, frac(32), frac(66), frac(68), frac(86), glyphColor)  // paper output
	fillRect(img, frac(66), frac(42), frac(74), frac(50), accentColor) // status light

	return img
}

// downsample box-filters src (targetSize*supersample square) down to
// targetSize, averaging in the same premultiplied-alpha space img.At()
// already returns (color.RGBA64 expects premultiplied too, matching it).
func downsample(src *image.RGBA, targetSize int) *image.RGBA {
	factor := src.Bounds().Dx() / targetSize
	dst := image.NewRGBA(image.Rect(0, 0, targetSize, targetSize))
	for y := 0; y < targetSize; y++ {
		for x := 0; x < targetSize; x++ {
			var rSum, gSum, bSum, aSum uint32
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					r, g, b, a := src.At(x*factor+dx, y*factor+dy).RGBA()
					rSum += r
					gSum += g
					bSum += b
					aSum += a
				}
			}
			n := uint32(factor * factor)
			dst.Set(x, y, color.RGBA64{
				R: uint16(rSum / n), G: uint16(gSum / n), B: uint16(bSum / n), A: uint16(aSum / n),
			})
		}
	}
	return dst
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.Set(x, y, c)
		}
	}
}

// roundedRectMask reports, for a size x size square with the given corner
// radius, whether pixel (x,y) falls inside the rounded rectangle (true) or
// in one of the four clipped corners (false).
func roundedRectMask(size, radius int) func(x, y int) bool {
	return func(x, y int) bool {
		cx, cy := -1, -1
		if x < radius {
			cx = radius
		} else if x >= size-radius {
			cx = size - radius - 1
		}
		if y < radius {
			cy = radius
		} else if y >= size-radius {
			cy = size - radius - 1
		}
		if cx == -1 || cy == -1 {
			return true // not in a corner region at all
		}
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= radius*radius
	}
}

// writeICO writes a minimal but valid multi-image ICO container, each frame
// PNG-compressed (BI_PNG, supported since Windows Vista) rather than the
// classic uncompressed BMP+AND-mask format.
func writeICO(w *os.File, images []image.Image) error {
	type frame struct {
		data []byte
		w, h int
	}
	frames := make([]frame, len(images))
	for i, img := range images {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		b := img.Bounds()
		frames[i] = frame{data: buf.Bytes(), w: b.Dx(), h: b.Dy()}
	}

	// ICONDIR
	if err := binary.Write(w, binary.LittleEndian, uint16(0)); err != nil { // reserved
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // type = icon
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(len(frames))); err != nil {
		return err
	}

	dimByte := func(n int) byte {
		if n >= 256 {
			return 0 // 0 means 256 in ICO's one-byte dimension field
		}
		return byte(n)
	}

	offset := uint32(6 + 16*len(frames))
	for _, f := range frames {
		if err := binary.Write(w, binary.LittleEndian, dimByte(f.w)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, dimByte(f.h)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil { // color count
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil { // reserved
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // color planes
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint16(32)); err != nil { // bits per pixel
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(f.data))); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, offset); err != nil {
			return err
		}
		offset += uint32(len(f.data))
	}
	for _, f := range frames {
		if _, err := w.Write(f.data); err != nil {
			return err
		}
	}
	return nil
}
