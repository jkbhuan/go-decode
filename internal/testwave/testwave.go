// Package testwave is a test-only, controllable analog waveform synthesizer.
//
// It is intentionally independent of the oscilloscope and of any vendor
// format. Every transition time is specified explicitly, so protocol decoding
// and timing calculation can be tested without a signal source. The package
// enables the receiver-side tests (external test packages) but is not part of
// the library's public API.
package testwave

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/jkbhuan/go-decode/core"
)

// Config describes one synthesized analog channel.
type Config struct {
	// StartTime is the absolute instrument time of sample zero. Negative and
	// non-zero values are supported on purpose.
	StartTime float64
	// SampleRate is in samples per second.
	SampleRate float64
	// Duration is the length of the record in seconds.
	Duration float64
	// Low and High are the steady-state voltages of the two logic levels.
	Low  float64
	High float64
	// RiseTime and FallTime are the 0%-100% linear ramp durations. Zero means
	// an ideal step. A transition's midpoint (50%) is at its specified time.
	RiseTime float64
	FallTime float64
	// Noise is the peak amplitude of uniformly distributed additive noise.
	Noise float64
	// Jitter is the peak random offset applied to each transition time.
	Jitter float64
	// Seed makes noise and jitter reproducible.
	Seed int64
}

// Transition is a digital level change at an absolute time. The specified time
// is the 50%-crossing midpoint of the analog ramp.
type Transition struct {
	T    float64
	High bool
}

// Validate checks the synthesizer configuration.
func (c Config) Validate() error {
	if !finite(c.StartTime) {
		return fmt.Errorf("testwave: start time must be finite")
	}
	if !finite(c.SampleRate) || c.SampleRate <= 0 {
		return fmt.Errorf("testwave: sample rate must be positive and finite")
	}
	if !finite(c.Duration) || c.Duration < 0 {
		return fmt.Errorf("testwave: duration must be non-negative and finite")
	}
	if !finite(c.Low) || !finite(c.High) || c.High <= c.Low {
		return fmt.Errorf("testwave: high level must be greater than low level")
	}
	if c.RiseTime < 0 || c.FallTime < 0 || c.Noise < 0 || c.Jitter < 0 {
		return fmt.Errorf("testwave: times, noise and jitter must be non-negative")
	}
	if !finite(c.RiseTime) || !finite(c.FallTime) || !finite(c.Noise) || !finite(c.Jitter) {
		return fmt.Errorf("testwave: noise and jitter must be finite")
	}
	return nil
}

// Build synthesizes one channel with the given initial logic level and
// transitions.
func Build(cfg Config, initialHigh bool, transitions []Transition) (core.Waveform, error) {
	if err := cfg.Validate(); err != nil {
		return core.Waveform{}, err
	}
	if cfg.Duration <= 0 {
		return core.Waveform{}, fmt.Errorf("testwave: duration must be positive")
	}
	n := int(math.Floor(cfg.Duration*cfg.SampleRate+1e-9)) + 1
	if n < 2 {
		return core.Waveform{}, fmt.Errorf("testwave: configuration yields fewer than 2 samples")
	}

	trans := append([]Transition(nil), transitions...)
	if cfg.Jitter > 0 || cfg.Seed != 0 {
		rng := rand.New(rand.NewSource(cfg.Seed))
		for i := range trans {
			if cfg.Jitter > 0 {
				trans[i].T += (rng.Float64()*2 - 1) * cfg.Jitter
			}
		}
	}
	sort.SliceStable(trans, func(i, j int) bool { return trans[i].T < trans[j].T })

	var rng *rand.Rand
	if cfg.Noise > 0 {
		rng = rand.New(rand.NewSource(cfg.Seed ^ 0x5bd1e995))
	}

	samples := make([]float64, n)
	for i := 0; i < n; i++ {
		t := cfg.StartTime + float64(i)/cfg.SampleRate
		unit := levelAt(t, initialHigh, trans, cfg)
		v := cfg.Low + (cfg.High-cfg.Low)*unit
		if rng != nil {
			v += (rng.Float64()*2 - 1) * cfg.Noise
		}
		samples[i] = v
	}
	w := core.Waveform{Samples: samples, SampleRate: cfg.SampleRate, StartTime: cfg.StartTime}
	if err := w.Validate(); err != nil {
		return core.Waveform{}, err
	}
	return w, nil
}

func levelAt(t float64, initialHigh bool, ts []Transition, cfg Config) float64 {
	n := len(ts)
	j := sort.Search(n, func(i int) bool { return ts[i].T > t }) - 1

	// Is the next transition already inside its ramp?
	if j+1 < n {
		next := ts[j+1]
		d := rampDuration(next.High, cfg)
		start := next.T - d/2
		if d > 0 && t >= start {
			frac := clamp01((t - start) / d)
			return lerp(levelOf(j, initialHigh, ts), boolLevel(next.High), frac)
		}
	}
	// Is the current transition still ramping?
	if j >= 0 {
		cur := ts[j]
		d := rampDuration(cur.High, cfg)
		start := cur.T - d/2
		if d > 0 && t < cur.T+d/2 {
			frac := clamp01((t - start) / d)
			return lerp(levelOf(j-1, initialHigh, ts), boolLevel(cur.High), frac)
		}
	}
	return levelOf(j, initialHigh, ts)
}

func levelOf(j int, initialHigh bool, ts []Transition) float64 {
	high := initialHigh
	if j >= 0 && j < len(ts) {
		high = ts[j].High
	}
	if high {
		return 1
	}
	return 0
}

func boolLevel(high bool) float64 {
	if high {
		return 1
	}
	return 0
}

func lerp(fromUnit, toUnit, frac float64) float64 {
	return fromUnit + (toUnit-fromUnit)*frac
}

func rampDuration(toHigh bool, cfg Config) float64 {
	if toHigh {
		return cfg.RiseTime
	}
	return cfg.FallTime
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
