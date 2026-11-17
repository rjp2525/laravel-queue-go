package main

import (
	"embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Social preview images (og:image), one per page, drawn at build time in the
// site's own look so a shared link is recognisable before it's opened.

//go:embed fonts/*.ttf
var fontFiles embed.FS

const ogW, ogH = 1200, 630

var (
	ogBG     = color.RGBA{0x01, 0x16, 0x27, 0xff}
	ogNavy   = color.RGBA{0x1e, 0x2d, 0x3d, 0xff}
	ogMenu   = color.RGBA{0x60, 0x7b, 0x96, 0xff}
	ogText   = color.RGBA{0xa5, 0xb8, 0xcc, 0xff}
	ogWhite  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ogTeal   = color.RGBA{0x43, 0xd9, 0xad, 0xff}
	ogOrange = color.RGBA{0xfe, 0xa5, 0x5f, 0xff}
	ogPurple = color.RGBA{0x7d, 0x86, 0xf0, 0xff}
)

type ogFonts struct {
	regular, semibold *opentype.Font
}

func loadOGFonts() (*ogFonts, error) {
	load := func(name string) (*opentype.Font, error) {
		data, err := fontFiles.ReadFile("fonts/" + name)
		if err != nil {
			return nil, err
		}
		return opentype.Parse(data)
	}
	regular, err := load("GeistMono-Regular.ttf")
	if err != nil {
		return nil, err
	}
	semibold, err := load("GeistMono-SemiBold.ttf")
	if err != nil {
		return nil, err
	}
	return &ogFonts{regular: regular, semibold: semibold}, nil
}

func (f *ogFonts) face(semibold bool, size float64) font.Face {
	src := f.regular
	if semibold {
		src = f.semibold
	}
	face, err := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err) // the fonts are embedded; a failure here is a build bug
	}
	return face
}

// ogCard is the text that goes on one image.
type ogCard struct {
	Kicker      string // "// docs / consuming"
	Title       string
	Description string
	Tab         string // the tab drawn as active
}

func writeOGImage(fonts *ogFonts, path string, card ogCard) error {
	img := image.NewRGBA(image.Rect(0, 0, ogW, ogH))
	draw.Draw(img, img.Bounds(), image.NewUniform(ogBG), image.Point{}, draw.Src)

	// The two soft glows from the landing page.
	glow(img, 820, 150, 330, color.RGBA{0x43, 0xd9, 0xad, 0xff}, 0.20)
	glow(img, 1090, 380, 360, color.RGBA{0x4d, 0x5b, 0xce, 0xff}, 0.30)

	// The framed window.
	const m = 36 // margin around the frame
	frame := image.Rect(m, m, ogW-m, ogH-m)
	outline(img, frame, ogNavy)

	// Top bar with tabs.
	const barH = 58
	hline(img, frame.Min.X, frame.Max.X, frame.Min.Y+barH, ogNavy)
	small := fonts.face(false, 20)
	x := frame.Min.X
	for i, label := range []string{"laravel-queue-go", "_hello", "_docs", "_laravel", "_upgrading"} {
		w := textWidth(small, label) + 56
		if i == 0 {
			w += 40
		}
		active := label == card.Tab
		col := ogMenu
		if active {
			col = ogWhite
			fill(img, image.Rect(x, frame.Min.Y+barH-3, x+w, frame.Min.Y+barH), ogOrange)
		}
		drawText(img, small, label, x+28, frame.Min.Y+37, col)
		x += w
		vline(img, x, frame.Min.Y, frame.Min.Y+barH, ogNavy)
	}

	// Bottom bar.
	const footH = 52
	hline(img, frame.Min.X, frame.Max.X, frame.Max.Y-footH, ogNavy)
	drawText(img, small, "laravel-queue-go.reno.sh", frame.Min.X+28, frame.Max.Y-18, ogMenu)
	right := "laravel 10 · 11 · 12 · 13"
	drawText(img, small, right, frame.Max.X-28-textWidth(small, right), frame.Max.Y-18, ogMenu)

	// Body: kicker, title, description, vertically centred as a block.
	left := frame.Min.X + 56
	width := frame.Dx() - 112
	kickerFace := fonts.face(false, 24)
	titleSize := 68.0
	titleFace := fonts.face(true, titleSize)
	titleLines := wrap(titleFace, card.Title, width)
	for len(titleLines) > 2 && titleSize > 44 {
		titleSize -= 6
		titleFace = fonts.face(true, titleSize)
		titleLines = wrap(titleFace, card.Title, width)
	}
	descFace := fonts.face(false, 28)
	descLines := wrap(descFace, card.Description, width)
	if len(descLines) > 3 {
		descLines = descLines[:3]
		descLines[2] = strings.TrimRight(descLines[2], " .,;:") + "…"
	}

	titleLH := int(titleSize * 1.16)
	const descLH = 42
	blockH := 34 + 26 + len(titleLines)*titleLH + 22 + len(descLines)*descLH
	top := frame.Min.Y + barH
	bottom := frame.Max.Y - footH
	y := top + (bottom-top-blockH)/2 + 26

	drawText(img, kickerFace, card.Kicker, left, y, ogTeal)
	y += 26
	for _, line := range titleLines {
		y += titleLH
		drawText(img, titleFace, line, left, y, ogWhite)
	}
	y += 22
	descColor := ogText
	if strings.HasPrefix(card.Description, ">") {
		descColor = ogPurple // the landing page's prompt line
	}
	for _, line := range descLines {
		y += descLH
		drawText(img, descFace, line, left, y, descColor)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img)
}

func drawText(img *image.RGBA, face font.Face, s string, x, y int, col color.Color) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func textWidth(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

// wrap breaks s into lines no wider than width pixels.
func wrap(face font.Face, s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if line != "" && textWidth(face, candidate) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line = candidate
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func fill(img *image.RGBA, r image.Rectangle, col color.Color) {
	draw.Draw(img, r, image.NewUniform(col), image.Point{}, draw.Src)
}

func hline(img *image.RGBA, x0, x1, y int, col color.Color) {
	fill(img, image.Rect(x0, y, x1, y+2), col)
}

func vline(img *image.RGBA, x, y0, y1 int, col color.Color) {
	fill(img, image.Rect(x, y0, x+2, y1), col)
}

func outline(img *image.RGBA, r image.Rectangle, col color.Color) {
	hline(img, r.Min.X, r.Max.X, r.Min.Y, col)
	hline(img, r.Min.X, r.Max.X, r.Max.Y-2, col)
	vline(img, r.Min.X, r.Min.Y, r.Max.Y, col)
	vline(img, r.Max.X-2, r.Min.Y, r.Max.Y, col)
}

// glow blends a soft radial blob of colour over the background.
func glow(img *image.RGBA, cx, cy, radius int, col color.RGBA, strength float64) {
	for y := max(cy-radius, 0); y < min(cy+radius, ogH); y++ {
		for x := max(cx-radius, 0); x < min(cx+radius, ogW); x++ {
			d := math.Hypot(float64(x-cx), float64(y-cy)) / float64(radius)
			if d >= 1 {
				continue
			}
			a := strength * math.Pow(1-d, 2)
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8(float64(img.Pix[i+0])*(1-a) + float64(col.R)*a)
			img.Pix[i+1] = uint8(float64(img.Pix[i+1])*(1-a) + float64(col.G)*a)
			img.Pix[i+2] = uint8(float64(img.Pix[i+2])*(1-a) + float64(col.B)*a)
		}
	}
}
