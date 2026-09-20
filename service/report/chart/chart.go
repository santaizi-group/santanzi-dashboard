package chart

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	resfont "github.com/hi2shark/santaizi-dashboard/resource/font"
)

type Palette struct {
	Background color.RGBA
	Surface    color.RGBA
	Text       color.RGBA
	Muted      color.RGBA
	Grid       color.RGBA
	Primary    color.RGBA
	Success    color.RGBA
	Warning    color.RGBA
	Danger     color.RGBA
}

func LightPalette() Palette {
	return Palette{
		Background: color.RGBA{R: 0xf5, G: 0xf7, B: 0xfb, A: 0xff},
		Surface:    color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		Text:       color.RGBA{R: 0x17, G: 0x20, B: 0x33, A: 0xff},
		Muted:      color.RGBA{R: 0x66, G: 0x70, B: 0x85, A: 0xff},
		Grid:       color.RGBA{R: 0xe4, G: 0xe9, B: 0xf2, A: 0xff},
		Primary:    color.RGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff},
		Success:    color.RGBA{R: 0x05, G: 0x96, B: 0x69, A: 0xff},
		Warning:    color.RGBA{R: 0xd9, G: 0x77, B: 0x06, A: 0xff},
		Danger:     color.RGBA{R: 0xdc, G: 0x26, B: 0x26, A: 0xff},
	}
}

func DarkPalette() Palette {
	return Palette{
		Background: color.RGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 0xff},
		Surface:    color.RGBA{R: 0x1e, G: 0x29, B: 0x3b, A: 0xff},
		Text:       color.RGBA{R: 0xf1, G: 0xf5, B: 0xf9, A: 0xff},
		Muted:      color.RGBA{R: 0x94, G: 0xa3, B: 0xb8, A: 0xff},
		Grid:       color.RGBA{R: 0x33, G: 0x41, B: 0x55, A: 0xff},
		Primary:    color.RGBA{R: 0x60, G: 0xa5, B: 0xfa, A: 0xff},
		Success:    color.RGBA{R: 0x34, G: 0xd3, B: 0x99, A: 0xff},
		Warning:    color.RGBA{R: 0xfb, G: 0xbf, B: 0x24, A: 0xff},
		Danger:     color.RGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff},
	}
}

type Series struct {
	Name   string
	Points []float64
	Color  color.RGBA
}

type BarItem struct {
	Label string
	Value float64
	Color color.RGBA
}

var (
	faceOnce  sync.Once
	labelFace font.Face
	titleFace font.Face
)

func faces() (font.Face, font.Face) {
	faceOnce.Do(func() {
		src := resfont.TTF()
		parsed, err := opentype.Parse(src)
		if err != nil {
			parsed, _ = opentype.Parse(goregular.TTF)
		}
		labelFace, _ = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 13, DPI: 144, Hinting: font.HintingFull})
		titleFace, _ = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 16, DPI: 144, Hinting: font.HintingFull})
	})
	return labelFace, titleFace
}

func Bar(title string, items []BarItem) ([]byte, error) {
	return BarTheme(title, items, LightPalette())
}

func BarTheme(title string, items []BarItem, p Palette) ([]byte, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) > 12 {
		items = items[:12]
	}
	const scale = 2
	width, height := 900*scale, (120+len(items)*36)*scale
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: p.Background}, image.Point{}, draw.Src)
	label, heading := faces()
	drawString(img, heading, p.Text, 24*scale, 36*scale, displayText(title))
	maxVal := 1.0
	for _, item := range items {
		if item.Value > maxVal {
			maxVal = item.Value
		}
	}
	left := 220 * scale
	right := width - 40*scale
	barW := right - left
	for i, item := range items {
		y := (70 + i*36) * scale
		drawString(img, label, p.Text, 24*scale, y+20*scale, displayText(truncate(item.Label, 16)))
		fill := item.Color
		if fill.A == 0 {
			fill = p.Primary
		}
		w := int(float64(barW) * item.Value / maxVal)
		if w < 2*scale {
			w = 2 * scale
		}
		rect(img, left, y, left+w, y+22*scale, fill)
		drawString(img, label, p.Muted, left+w+8*scale, y+18*scale, formatNum(item.Value))
	}
	return encode(img)
}

func Line(title string, series []Series, labels []string) ([]byte, error) {
	return LineTheme(title, series, labels, LightPalette())
}

func LineTheme(title string, series []Series, labels []string, p Palette) ([]byte, error) {
	if len(series) == 0 {
		return nil, nil
	}
	const scale = 2
	width, height := 900*scale, 420*scale
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: p.Background}, image.Point{}, draw.Src)
	label, heading := faces()
	drawString(img, heading, p.Text, 24*scale, 36*scale, displayText(title))
	plot := image.Rect(80*scale, 60*scale, width-30*scale, height-50*scale)
	rect(img, plot.Min.X, plot.Min.Y, plot.Max.X, plot.Max.Y, p.Surface)
	maxVal := 1.0
	n := 0
	for _, s := range series {
		if len(s.Points) > n {
			n = len(s.Points)
		}
		for _, v := range s.Points {
			if v > maxVal {
				maxVal = v
			}
		}
	}
	if n < 2 {
		n = 2
	}
	for i := 0; i <= 4; i++ {
		y := plot.Max.Y - i*(plot.Dy())/4
		hline(img, plot.Min.X, plot.Max.X, y, p.Grid)
		drawString(img, label, p.Muted, 16*scale, y+4*scale, formatNum(maxVal*float64(i)/4))
	}
	colors := []color.RGBA{p.Primary, p.Success, p.Warning, p.Danger}
	for si, s := range series {
		col := s.Color
		if col.A == 0 {
			col = colors[si%len(colors)]
		}
		prev := image.Point{}
		span := n - 1
		if span < 1 {
			span = 1
		}
		for i, v := range s.Points {
			x := plot.Min.X + i*plot.Dx()/span
			y := plot.Max.Y - int(float64(plot.Dy())*v/maxVal)
			pt := image.Point{X: x, Y: y}
			if i > 0 {
				line(img, prev, pt, col)
			}
			prev = pt
		}
		drawString(img, label, col, plot.Min.X+si*160*scale, height-18*scale, displayText(s.Name))
	}
	if len(labels) > 0 {
		drawString(img, label, p.Muted, plot.Min.X, height-18*scale, displayText(labels[0]))
		drawString(img, label, p.Muted, plot.Max.X-120*scale, height-18*scale, displayText(labels[len(labels)-1]))
	}
	return encode(img)
}

func encode(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func hline(img *image.RGBA, x0, x1, y int, c color.RGBA) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		img.SetRGBA(x, y, c)
	}
}

func line(img *image.RGBA, a, b image.Point, c color.RGBA) {
	dx := abs(b.X - a.X)
	dy := abs(b.Y - a.Y)
	sx, sy := 1, 1
	if a.X > b.X {
		sx = -1
	}
	if a.Y > b.Y {
		sy = -1
	}
	err := dx - dy
	x, y := a.X, a.Y
	for {
		img.SetRGBA(x, y, c)
		if x == b.X && y == b.Y {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func drawString(img *image.RGBA, face font.Face, c color.RGBA, x, y int, text string) {
	if face == nil || text == "" {
		return
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

func displayText(text string) string {
	if resfont.HasCJK() {
		return text
	}
	var b strings.Builder
	for _, r := range text {
		if r <= 0x7f {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('?')
	}
	return b.String()
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "..."
}

func formatNum(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.05 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
