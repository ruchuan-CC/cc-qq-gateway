package gateway

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// QQ Markdown cannot render bordered tables (no monospace, no pipe tables / code
// blocks), so a true "spreadsheet" table is drawn as a PNG and sent as an image.
// This file renders headers+rows into a clean grid image using a CJK TrueType
// font loaded from disk (path is configurable; the font lives outside the repo).

var (
	fontOnce sync.Once
	cjkFont  *opentype.Font
	fontErr  error
)

// loadCJKFont parses the configured CJK font once and caches it.
func loadCJKFont(path string) (*opentype.Font, error) {
	fontOnce.Do(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			fontErr = fmt.Errorf("read font %s: %w", path, err)
			return
		}
		cjkFont, fontErr = opentype.Parse(data)
	})
	return cjkFont, fontErr
}

// table image palette and layout (in pixels).
var (
	clrBG     = color.RGBA{255, 255, 255, 255}
	clrGrid   = color.RGBA{206, 212, 218, 255}
	clrHeadBG = color.RGBA{240, 244, 248, 255}
	clrText   = color.RGBA{33, 37, 41, 255}
	clrHead   = color.RGBA{17, 24, 39, 255}
)

const (
	imgFontSize = 26.0
	imgDPI      = 96.0
	imgPadX     = 22
	imgPadY     = 14
	imgGrid     = 2
)

// renderTableImagePNG draws an optional title banner plus headers + rows as a
// bordered grid and writes a PNG to outPath. Column widths fit the widest cell;
// the header row is shaded.
func renderTableImagePNG(fontPath, title string, headers []string, rows [][]string, outPath string) error {
	f, err := loadCJKFont(fontPath)
	if err != nil {
		return err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: imgFontSize, DPI: imgDPI, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	defer face.Close()
	titleFace, err := opentype.NewFace(f, &opentype.FaceOptions{Size: imgFontSize * 1.3, DPI: imgDPI, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	defer titleFace.Close()

	all := append([][]string{headers}, rows...)
	cols := len(headers)

	colW := make([]int, cols)
	for _, row := range all {
		for c := 0; c < cols; c++ {
			if w := cellWidth(face, cellAt(row, c)); w > colW[c] {
				colW[c] = w
			}
		}
	}
	for c := range colW {
		colW[c] += 2 * imgPadX
	}

	m := face.Metrics()
	ascent, descent := m.Ascent.Ceil(), m.Descent.Ceil()
	rowH := ascent + descent + 2*imgPadY

	width := imgGrid
	for _, w := range colW {
		width += w + imgGrid
	}

	// Optional title banner across the full width.
	tm := titleFace.Metrics()
	titleH := 0
	if title != "" {
		titleH = tm.Ascent.Ceil() + tm.Descent.Ceil() + 2*imgPadY + imgGrid
	}
	height := titleH + imgGrid + len(all)*(rowH+imgGrid)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill the whole canvas with the grid color, then paint each cell interior —
	// the imgGrid-px gaps left around cells become the grid lines automatically.
	draw.Draw(img, img.Bounds(), &image.Uniform{clrGrid}, image.Point{}, draw.Src)

	if title != "" {
		bh := titleH - imgGrid
		fillRect(img, imgGrid, imgGrid, width-imgGrid, bh, clrHeadBG)
		td := &font.Drawer{Dst: img, Src: &image.Uniform{clrHead}, Face: titleFace}
		tw := font.MeasureString(titleFace, title).Ceil()
		td.Dot = fixed.P((width-tw)/2, imgGrid+imgPadY+tm.Ascent.Ceil())
		td.DrawString(title)
	}

	drawer := &font.Drawer{Dst: img, Face: face}
	y := titleH + imgGrid
	for r, row := range all {
		bg, fg := clrBG, clrText
		if r == 0 {
			bg, fg = clrHeadBG, clrHead
		}
		x := imgGrid
		for c := 0; c < cols; c++ {
			fillRect(img, x, y, x+colW[c], y+rowH, bg)
			drawer.Src = &image.Uniform{fg}
			drawer.Dot = fixed.P(x+imgPadX, y+imgPadY+ascent)
			drawer.DrawString(cellAt(row, c))
			x += colW[c] + imgGrid
		}
		y += rowH + imgGrid
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, img)
}

func cellAt(row []string, c int) string {
	if c < len(row) {
		return row[c]
	}
	return ""
}

func cellWidth(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, col color.Color) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{col}, image.Point{}, draw.Src)
}
