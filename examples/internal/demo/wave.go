// Package demo contains a minimal ideal-step waveform builder used only by the
// example programs. It is intentionally not the test synthesizer: examples
// must not depend on test-only code.
package demo

import (
	"fmt"
	"math"
	"sort"

	"github.com/jkbhuan/go-decode"
)

// Transition is a digital level change at an absolute time.
type Transition struct {
	T    float64
	High bool
}

// Config describes an ideal-step channel.
type Config struct {
	StartTime  float64
	SampleRate float64
	Duration   float64
	Low        float64
	High       float64
}

// Build creates an ideal (zero-rise-time) waveform from transitions.
func Build(cfg Config, initialHigh bool, transitions []Transition) (decode.Waveform, error) {
	if !finite(cfg.StartTime) || !finite(cfg.SampleRate) || cfg.SampleRate <= 0 ||
		!finite(cfg.Duration) || cfg.Duration <= 0 ||
		!finite(cfg.Low) || !finite(cfg.High) || cfg.High <= cfg.Low {
		return decode.Waveform{}, fmt.Errorf("demo: invalid waveform configuration")
	}
	n := int(math.Floor(cfg.Duration*cfg.SampleRate+1e-9)) + 1
	if n < 2 {
		return decode.Waveform{}, fmt.Errorf("demo: configuration yields fewer than 2 samples")
	}
	ts := append([]Transition(nil), transitions...)
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].T < ts[j].T })

	high := initialHigh
	next := 0
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		t := cfg.StartTime + float64(i)/cfg.SampleRate
		for next < len(ts) && ts[next].T <= t {
			high = ts[next].High
			next++
		}
		if high {
			samples[i] = float32(cfg.High)
		} else {
			samples[i] = float32(cfg.Low)
		}
	}
	w := decode.Waveform{Samples: samples, SampleRate: cfg.SampleRate, StartTime: cfg.StartTime}
	if err := w.Validate(); err != nil {
		return decode.Waveform{}, err
	}
	return w, nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
