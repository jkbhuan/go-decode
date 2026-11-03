package waveplot_test

import (
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/waveplot"
)

func TestRenderDigitalAndAnalog(t *testing.T) {
	// Sparse waveform exercises the polyline path; dense exercises the
	// per-column min/max envelope.
	sparse := make([]float32, 200)
	dense := make([]float32, 5000)
	for i := range sparse {
		if i%20 < 10 {
			sparse[i] = 3.3
		}
	}
	for i := range dense {
		dense[i] = float32(1.65 + 1.4*math.Sin(float64(i)*0.01))
	}
	scl := decode.Waveform{Samples: sparse, SampleRate: 1e6, StartTime: -50e-6}
	sda := decode.Waveform{Samples: dense, SampleRate: 1e6, StartTime: -50e-6}

	img := waveplot.Render("TEST: CH1=SCL CH2=SDA", []waveplot.Channel{
		{Label: "CH1 / SCL", Wave: scl, Color: color.RGBA{31, 119, 180, 255}, Analog: true},
		{Label: "CH2 / SDA", Wave: sda, Color: color.RGBA{214, 39, 40, 255}, Analog: true},
	})
	if img.Bounds().Dx() != 1800 || img.Bounds().Dy() != 524 {
		t.Fatalf("unexpected image size: %v", img.Bounds())
	}

	// The two channel colors must appear somewhere in the image.
	blue, red := false, false
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			switch {
			case r == 31*257 && g == 119*257 && b == 180*257:
				blue = true
			case r == 214*257 && g == 39*257 && b == 40*257:
				red = true
			}
		}
	}
	if !blue || !red {
		t.Fatalf("channel colors missing: blue=%v red=%v", blue, red)
	}

	path := filepath.Join(t.TempDir(), "nested", "capture.png")
	if err := waveplot.WritePNG(path, "TEST", []waveplot.Channel{
		{Label: "CH1 / SCL", Wave: scl, Color: color.RGBA{31, 119, 180, 255}, Analog: true},
	}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("PNG not written: %v", err)
	}
}

func TestRenderNoChannels(t *testing.T) {
	img := waveplot.Render("EMPTY", nil)
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Fatalf("unexpected empty image: %v", img.Bounds())
	}
	if err := waveplot.WritePNG(filepath.Join(t.TempDir(), "x.png"), "EMPTY", nil); err == nil {
		t.Fatal("expected error for no channels")
	}
}
