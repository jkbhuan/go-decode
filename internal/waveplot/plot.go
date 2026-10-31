package waveplot

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"unicode"

	"github.com/jkbhuan/go-decode"
)

// Channel is one labeled waveform lane in an image.
//
// Analog false draws a logic-step lane around the midpoint (useful for a
// synthesized bus). Analog true draws the actual sampled voltage envelope,
// which preserves overshoot, ringing and noise from a real scope capture.
type Channel struct {
	Label  string
	Wave   decode.Waveform
	Color  color.RGBA
	Analog bool
}

var (
	colBackground = color.RGBA{255, 255, 255, 255}
	colPlot       = color.RGBA{249, 250, 252, 255}
	colGrid       = color.RGBA{221, 225, 231, 255}
	colAxis       = color.RGBA{86, 93, 102, 255}
	colText       = color.RGBA{33, 38, 45, 255}
	colMuted      = color.RGBA{110, 118, 129, 255}
)

// WritePNG renders a multi-lane waveform image and writes it as a PNG. It uses
// only the Go standard library.
func WritePNG(path, title string, channels []Channel) error {
	if len(channels) == 0 {
		return fmt.Errorf("waveplot: no channels to plot")
	}
	img := Render(title, channels)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Render draws a simple oscilloscope-style diagram: one lane per channel, a
// shared absolute time axis in microseconds, channel labels and a title.
// Digital lanes are drawn as ideal logic steps around the midpoint; analog
// lanes draw the sampled voltage envelope.
func Render(title string, channels []Channel) *image.RGBA {
	if len(channels) == 0 {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.SetRGBA(0, 0, colBackground)
		return img
	}
	const (
		width      = 1800
		laneHeight = 170
		topMargin  = 92
		botMargin  = 92
		leftMargin = 150
		rightMarg  = 45
	)
	height := topMargin + laneHeight*len(channels) + botMargin
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(img, 0, 0, width, height, colBackground)

	plotLeft, plotRight := leftMargin, width-rightMarg
	plotTop, plotBottom := topMargin, height-botMargin
	fillRect(img, plotLeft, plotTop, plotRight, plotBottom, colPlot)

	tMin, tMax := timeRange(channels)
	if tMax <= tMin {
		tMax = tMin + 1e-6
	}
	xAt := func(t float64) int {
		f := (t - tMin) / (tMax - tMin)
		x := plotLeft + int(f*float64(plotRight-plotLeft))
		return clamp(x, plotLeft, plotRight)
	}

	// Title.
	drawTextScaled(img, 24, 22, title, 3, colText)
	drawTextScaled(img, plotLeft, plotBottom+52, "TIME (US)", 2, colMuted)

	// Vertical time grid and tick labels.
	const ticks = 10
	rangeUs := (tMax - tMin) / 1e-6
	decimals := 0
	switch {
	case rangeUs < 5:
		decimals = 3
	case rangeUs < 50:
		decimals = 2
	case rangeUs < 500:
		decimals = 1
	}
	for i := 0; i <= ticks; i++ {
		f := float64(i) / ticks
		x := plotLeft + int(f*float64(plotRight-plotLeft))
		fillRect(img, x, plotTop, x+1, plotBottom, colGrid)
		t := tMin + f*(tMax-tMin)
		label := fmt.Sprintf("%.*f", decimals, t/1e-6)
		lw := textWidth(label, 2)
		drawTextScaled(img, clamp(x-lw/2, 2, width-lw-2), plotBottom+22, label, 2, colMuted)
	}

	// Lanes.
	for ci, ch := range channels {
		laneTop := plotTop + ci*laneHeight
		laneBottom := laneTop + laneHeight
		highY := laneTop + 38
		lowY := laneBottom - 38

		if ci > 0 {
			fillRect(img, plotLeft, laneTop, plotRight, laneTop+1, colGrid)
		}
		drawTextScaled(img, 20, laneTop+laneHeight/2-7, ch.Label, 2, ch.Color)
		if ch.Analog {
			drawAnalogLane(img, ch, xAt, plotLeft, plotRight, laneTop+18, laneBottom-18)
		} else {
			fillRect(img, plotLeft, highY, plotRight, highY+1, colGrid)
			fillRect(img, plotLeft, lowY, plotRight, lowY+1, colGrid)
			drawWaveLane(img, ch, xAt, plotLeft, plotRight, highY, lowY)
		}
	}

	// Plot border.
	fillRect(img, plotLeft, plotTop, plotRight, plotTop+2, colAxis)
	fillRect(img, plotLeft, plotBottom-2, plotRight, plotBottom, colAxis)
	fillRect(img, plotLeft, plotTop, plotLeft+2, plotBottom, colAxis)
	fillRect(img, plotRight-2, plotTop, plotRight, plotBottom, colAxis)
	return img
}

func drawAnalogLane(img *image.RGBA, ch Channel, xAt func(float64) int, left, right, top, bottom int) {
	w := ch.Wave
	if len(w.Samples) == 0 || right <= left || bottom <= top {
		return
	}
	minV, maxV := w.Samples[0], w.Samples[0]
	for _, v := range w.Samples {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= minV {
		maxV = minV + 1
	}
	pad := (maxV - minV) * 0.05
	minV -= pad
	maxV += pad
	yAt := func(v float64) int {
		f := (v - minV) / (maxV - minV)
		return clamp(bottom-int(f*float64(bottom-top)), top, bottom)
	}

	n := len(w.Samples)
	widthPx := right - left
	if n >= widthPx {
		// Deep-memory capture: draw a min/max envelope per screen column so
		// narrow spikes are preserved without drawing millions of segments.
		minY := make([]int, widthPx)
		maxY := make([]int, widthPx)
		seen := make([]bool, widthPx)
		for i, v := range w.Samples {
			x := xAt(w.StartTime+float64(i)/w.SampleRate) - left
			if x < 0 {
				x = 0
			} else if x >= widthPx {
				x = widthPx - 1
			}
			y := yAt(v)
			if !seen[x] {
				seen[x] = true
				minY[x], maxY[x] = y, y
			} else {
				if y < minY[x] {
					minY[x] = y
				}
				if y > maxY[x] {
					maxY[x] = y
				}
			}
		}
		for x := 0; x < widthPx; x++ {
			if seen[x] {
				fillRect(img, left+x, minY[x]-1, left+x+1, maxY[x]+2, ch.Color)
			}
		}
		return
	}
	prevX, prevY := 0, 0
	for i, v := range w.Samples {
		x := xAt(w.StartTime + float64(i)/w.SampleRate)
		y := yAt(v)
		if i == 0 {
			prevX, prevY = x, y
			continue
		}
		drawLine(img, prevX, prevY, x, y, ch.Color)
		prevX, prevY = x, y
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := absInt(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -absInt(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	b := img.Bounds()
	for {
		if x0 >= b.Min.X && x0 < b.Max.X && y0 >= b.Min.Y && y0 < b.Max.Y {
			img.SetRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func drawWaveLane(img *image.RGBA, ch Channel, xAt func(float64) int, left, right, highY, lowY int) {
	w := ch.Wave
	if len(w.Samples) == 0 {
		return
	}
	minV, maxV := w.Samples[0], w.Samples[0]
	for _, v := range w.Samples {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	mid := (minV + maxV) / 2
	high := w.Samples[0] >= mid
	prevX := left
	prevY := laneY(high, highY, lowY)

	for i := 1; i < len(w.Samples); i++ {
		h := w.Samples[i] >= mid
		if h == high {
			continue
		}
		t := w.StartTime + float64(i)/w.SampleRate
		x := xAt(t)
		if x <= prevX {
			x = prevX + 1
		}
		fillRect(img, prevX, prevY-1, x, prevY+2, ch.Color)
		y := laneY(h, highY, lowY)
		fillRect(img, x-1, minInt(prevY, y), x+2, maxInt(prevY, y)+1, ch.Color)
		prevX, prevY, high = x, y, h
	}
	fillRect(img, prevX, prevY-1, right, prevY+2, ch.Color)
}

func laneY(high bool, highY, lowY int) int {
	if high {
		return highY
	}
	return lowY
}

func timeRange(channels []Channel) (float64, float64) {
	tMin, tMax := channels[0].Wave.StartTime, channels[0].Wave.EndTime()
	for _, ch := range channels[1:] {
		if ch.Wave.StartTime < tMin {
			tMin = ch.Wave.StartTime
		}
		if ch.Wave.EndTime() > tMax {
			tMax = ch.Wave.EndTime()
		}
	}
	return tMin, tMax
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	x0, y0 = clamp(x0, b.Min.X, b.Max.X), clamp(y0, b.Min.Y, b.Max.Y)
	x1, y1 = clamp(x1, b.Min.X, b.Max.X), clamp(y1, b.Min.Y, b.Max.Y)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func textWidth(s string, scale int) int {
	return len([]rune(s))*6*scale - scale
}

func drawTextScaled(img *image.RGBA, x, y int, text string, scale int, c color.RGBA) {
	px := x
	for _, r := range text {
		glyph, ok := font5x7[unicode.ToUpper(r)]
		if !ok {
			glyph, ok = font5x7[' ']
		}
		if ok {
			for row := 0; row < 7; row++ {
				bits := glyph[row]
				for col := 0; col < 5; col++ {
					if bits&(1<<uint(4-col)) == 0 {
						continue
					}
					fillRect(img, px+col*scale, y+row*scale, px+(col+1)*scale, y+(row+1)*scale, c)
				}
			}
		}
		px += 6 * scale
	}
}

// font5x7 is a minimal 5x7 bitmap font covering the uppercase labels used by
// the examples. Bit 4 is the leftmost column.
var font5x7 = map[rune][7]uint8{
	' ': {0, 0, 0, 0, 0, 0, 0},
	'A': {0b01110, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
	'B': {0b11110, 0b10001, 0b10001, 0b11110, 0b10001, 0b10001, 0b11110},
	'C': {0b01111, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b01111},
	'D': {0b11110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b11110},
	'E': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b11111},
	'F': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b10000},
	'G': {0b01111, 0b10000, 0b10000, 0b10111, 0b10001, 0b10001, 0b01111},
	'H': {0b10001, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
	'I': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b11111},
	'J': {0b00111, 0b00010, 0b00010, 0b00010, 0b00010, 0b10010, 0b01100},
	'K': {0b10001, 0b10010, 0b10100, 0b11000, 0b10100, 0b10010, 0b10001},
	'L': {0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b11111},
	'M': {0b10001, 0b11011, 0b10101, 0b10101, 0b10001, 0b10001, 0b10001},
	'N': {0b10001, 0b11001, 0b10101, 0b10011, 0b10001, 0b10001, 0b10001},
	'O': {0b01110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
	'P': {0b11110, 0b10001, 0b10001, 0b11110, 0b10000, 0b10000, 0b10000},
	'Q': {0b01110, 0b10001, 0b10001, 0b10001, 0b10101, 0b10010, 0b01101},
	'R': {0b11110, 0b10001, 0b10001, 0b11110, 0b10100, 0b10010, 0b10001},
	'S': {0b01111, 0b10000, 0b10000, 0b01110, 0b00001, 0b00001, 0b11110},
	'T': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100},
	'U': {0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
	'V': {0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01010, 0b00100},
	'W': {0b10001, 0b10001, 0b10001, 0b10101, 0b10101, 0b10101, 0b01010},
	'X': {0b10001, 0b10001, 0b01010, 0b00100, 0b01010, 0b10001, 0b10001},
	'Y': {0b10001, 0b10001, 0b01010, 0b00100, 0b00100, 0b00100, 0b00100},
	'Z': {0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b10000, 0b11111},
	'0': {0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110},
	'1': {0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
	'2': {0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b01000, 0b11111},
	'3': {0b11111, 0b00010, 0b00100, 0b00010, 0b00001, 0b10001, 0b01110},
	'4': {0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010},
	'5': {0b11111, 0b10000, 0b11110, 0b00001, 0b00001, 0b10001, 0b01110},
	'6': {0b00110, 0b01000, 0b10000, 0b11110, 0b10001, 0b10001, 0b01110},
	'7': {0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000},
	'8': {0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110},
	'9': {0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00010, 0b01100},
	':': {0b00000, 0b00100, 0b00100, 0b00000, 0b00100, 0b00100, 0b00000},
	'+': {0b00000, 0b00100, 0b00100, 0b11111, 0b00100, 0b00100, 0b00000},
	'.': {0b00000, 0b00000, 0b00000, 0b00000, 0b00000, 0b01100, 0b01100},
	'-': {0b00000, 0b00000, 0b00000, 0b11111, 0b00000, 0b00000, 0b00000},
	'/': {0b00001, 0b00010, 0b00010, 0b00100, 0b01000, 0b01000, 0b10000},
	'(': {0b00010, 0b00100, 0b01000, 0b01000, 0b01000, 0b00100, 0b00010},
	')': {0b01000, 0b00100, 0b00010, 0b00010, 0b00010, 0b00100, 0b01000},
	'=': {0b00000, 0b00000, 0b11111, 0b00000, 0b11111, 0b00000, 0b00000},
	'?': {0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b00000, 0b00100},
	'#': {0b01010, 0b01010, 0b11111, 0b01010, 0b11111, 0b01010, 0b01010},
}
