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
	"golang.org/x/image/vector"

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

// seriesColors 返回按序可区分的 8 色；超过 8 条系列循环复用。
func (p Palette) seriesColors() []color.RGBA {
	return []color.RGBA{p.Primary, p.Success, p.Warning, p.Danger, p.extra1(), p.extra2(), p.extra3(), p.Muted}
}

func (p Palette) extra1() color.RGBA {
	if p.Background.R > 0x80 { // 浅色主题
		return color.RGBA{R: 0x7c, G: 0x3a, B: 0xed, A: 0xff} // 紫
	}
	return color.RGBA{R: 0xa7, G: 0x8b, B: 0xfa, A: 0xff}
}

func (p Palette) extra2() color.RGBA {
	if p.Background.R > 0x80 {
		return color.RGBA{R: 0x08, G: 0x91, B: 0xb2, A: 0xff} // 青
	}
	return color.RGBA{R: 0x22, G: 0xd3, B: 0xee, A: 0xff}
}

func (p Palette) extra3() color.RGBA {
	if p.Background.R > 0x80 {
		return color.RGBA{R: 0xdb, G: 0x27, B: 0x77, A: 0xff} // 品红
	}
	return color.RGBA{R: 0xf4, G: 0x72, B: 0xb6, A: 0xff}
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

// AxisFormat 决定坐标轴与数值标签的格式化方式。
type AxisFormat int

const (
	AxisNumber AxisFormat = iota // 智能位数
	AxisBytes                    // 字节自动 KB/MB/GB
)

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
	return BarFormat(title, items, AxisNumber, LightPalette())
}

func BarFormat(title string, items []BarItem, axis AxisFormat, p Palette) ([]byte, error) {
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
		text := formatAxis(item.Value, axis)
		textW := textWidth(label, text)
		if left+w+8*scale+textW > right {
			drawString(img, label, p.Surface, left+w-8*scale-textW, y+18*scale, text)
		} else {
			drawString(img, label, p.Muted, left+w+8*scale, y+18*scale, text)
		}
	}
	return encode(img)
}

func Line(title string, series []Series, labels []string) ([]byte, error) {
	return LineFormat(title, series, labels, AxisNumber, LightPalette())
}

func LineFormat(title string, series []Series, labels []string, axis AxisFormat, p Palette) ([]byte, error) {
	if len(series) == 0 {
		return nil, nil
	}
	const scale = 2
	width, height := 900*scale, 460*scale
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: p.Background}, image.Point{}, draw.Src)
	label, heading := faces()
	drawString(img, heading, p.Text, 24*scale, 36*scale, displayText(title))
	legendRows := drawLegend(img, label, series, p, width, 56*scale)
	plotTop := 56*scale + legendRows*26*scale + 10*scale
	plot := image.Rect(80*scale, plotTop, width-30*scale, height-50*scale)
	rect(img, plot.Min.X, plot.Min.Y, plot.Max.X, plot.Max.Y, p.Surface)
	maxVal := 0.0
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
	maxVal *= 1.1 // 顶部留头，最大点不贴边
	if maxVal <= 0 {
		maxVal = 1
	}
	if n < 2 {
		n = 2
	}
	for i := 0; i <= 4; i++ {
		y := plot.Max.Y - i*(plot.Dy())/4
		hline(img, plot.Min.X, plot.Max.X, y, p.Grid)
		drawString(img, label, p.Muted, 16*scale, y+4*scale, formatAxis(maxVal*float64(i)/4, axis))
	}
	colors := p.seriesColors()
	strokeW := 1.25 * scale
	for si := range series {
		s := &series[si]
		col := s.Color
		if col.A == 0 {
			col = colors[si%len(colors)]
		}
		span := n - 1
		if span < 1 {
			span = 1
		}
		pts := make([]image.Point, 0, len(s.Points))
		for i, v := range s.Points {
			x := plot.Min.X + i*plot.Dx()/span
			y := plot.Max.Y - int(float64(plot.Dy())*v/maxVal)
			pts = append(pts, image.Point{X: x, Y: y})
		}
		strokePolyline(img, pts, strokeW, col)
	}
	// 底部一行只放时间标签，与图例分离。
	if len(labels) > 0 {
		mid := labels[len(labels)/2]
		last := labels[len(labels)-1]
		drawString(img, label, p.Muted, plot.Min.X, height-18*scale, displayText(labels[0]))
		drawString(img, label, p.Muted, plot.Min.X+(plot.Dx()-textWidth(label, mid))/2, height-18*scale, displayText(mid))
		drawString(img, label, p.Muted, plot.Max.X-textWidth(label, last), height-18*scale, displayText(last))
	}
	return encode(img)
}

// drawLegend 在标题下方绘制图例（色块 + 名称），返回占用行数；最多两行，放不下省略。
func drawLegend(img *image.RGBA, face font.Face, series []Series, p Palette, width, y int) int {
	colors := p.seriesColors()
	const (
		swatch = 14 * 2
		gap    = 6 * 2
		item   = 20 * 2
	)
	x := 24 * 2
	maxX := width - 24*2
	row := 1
	drawSwatch(img, x, y, swatch, colorAt(colors, 0, series, 0))
	x += swatch + gap
	for si := range series {
		name := truncate(displayText(series[si].Name), 18)
		w := swatch + gap + textWidth(face, name)
		if x+w > maxX {
			if row >= 2 {
				break
			}
			row++
			y += 26 * 2
			x = 24 * 2
		}
		drawSwatch(img, x, y, swatch, colorAt(colors, si, series, si))
		x += swatch + gap
		drawString(img, face, p.Text, x, y+swatch-8, name)
		x += textWidth(face, name) + item
	}
	return row
}

func colorAt(colors []color.RGBA, fallback int, series []Series, si int) color.RGBA {
	if series[si].Color.A != 0 {
		return series[si].Color
	}
	return colors[fallback%len(colors)]
}

func drawSwatch(img *image.RGBA, x, y, size int, c color.RGBA) {
	rect(img, x, y, x+size, y+size/2, c)
}

func strokePolyline(img *image.RGBA, pts []image.Point, width float64, c color.RGBA) {
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		fillCircle(img, pts[0], width/2, c)
		return
	}
	r := &vector.Rasterizer{}
	r.Reset(img.Bounds().Dx(), img.Bounds().Dy())
	half := float32(width / 2)
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a == b {
			continue
		}
		dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
		l := math.Hypot(dx, dy)
		nx, ny := float32(-dy/l*float64(half)), float32(dx/l*float64(half))
		r.MoveTo(float32(a.X)+nx, float32(a.Y)+ny)
		r.LineTo(float32(b.X)+nx, float32(b.Y)+ny)
		r.LineTo(float32(b.X)-nx, float32(b.Y)-ny)
		r.LineTo(float32(a.X)-nx, float32(a.Y)-ny)
		r.ClosePath()
		r.MoveTo(float32(b.X)+half, float32(b.Y))
		addCircle(r, float32(b.X), float32(b.Y), half)
		r.ClosePath()
	}
	first := pts[0]
	r.MoveTo(float32(first.X)+half, float32(first.Y))
	addCircle(r, float32(first.X), float32(first.Y), half)
	r.ClosePath()
	r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
}

// addCircle 用四段三次贝塞尔近似整圆（路径须已 MoveTo 起点右侧；调用方负责 ClosePath）。
func addCircle(r *vector.Rasterizer, cx, cy, radius float32) {
	const k = 0.5522847498307936
	ck := radius * k
	r.CubeTo(cx+ck, cy+ck, cx+ck, cy+radius, cx, cy+radius)
	r.CubeTo(cx-ck, cy+radius, cx-radius, cy+ck, cx-radius, cy)
	r.CubeTo(cx-radius, cy-ck, cx-ck, cy-radius, cx, cy-radius)
	r.CubeTo(cx+ck, cy-radius, cx+ck, cy-radius+ck, cx+radius, cy)
}

func fillCircle(img *image.RGBA, p image.Point, radius float64, c color.RGBA) {
	r := &vector.Rasterizer{}
	r.Reset(img.Bounds().Dx(), img.Bounds().Dy())
	r.MoveTo(float32(p.X)+float32(radius), float32(p.Y))
	addCircle(r, float32(p.X), float32(p.Y), float32(radius))
	r.ClosePath()
	r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
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

func drawString(img *image.RGBA, face font.Face, c color.RGBA, x, y int, text string) {
	if face == nil || text == "" {
		return
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

func textWidth(face font.Face, text string) int {
	if face == nil {
		return 0
	}
	d := &font.Drawer{Face: face}
	return d.MeasureString(text).Ceil()
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

// formatAxis 坐标轴数值：字节自动单位，其余按量级取位数。
func formatAxis(v float64, axis AxisFormat) string {
	if axis == AxisBytes {
		return formatBytesAxis(v)
	}
	abs := math.Abs(v)
	switch {
	case abs >= 1000:
		return fmt.Sprintf("%.0f", v)
	case abs >= 100:
		return fmt.Sprintf("%.1f", v)
	case abs >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func formatBytesAxis(v float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	u := 0
	for v >= 1024 && u < len(units)-1 {
		v /= 1024
		u++
	}
	if u == 0 {
		return fmt.Sprintf("%.0fB", v)
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f%s", v, units[u])
	}
	return fmt.Sprintf("%.1f%s", v, units[u])
}
