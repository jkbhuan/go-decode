package decode_test

import (
	"errors"
	"math"
	"testing"

	"github.com/jkbhuan/go-decode"
)

func TestWaveformValidate(t *testing.T) {
	good := decode.Waveform{Samples: []float64{0, 1, 2}, SampleRate: 1e6, StartTime: -1e-6}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid waveform rejected: %v", err)
	}

	cases := []struct {
		name string
		w    decode.Waveform
		want error
	}{
		{"empty", decode.Waveform{SampleRate: 1, StartTime: 0}, decode.ErrEmptyWaveform},
		{"zero rate", decode.Waveform{Samples: []float64{0}, StartTime: 0}, decode.ErrInvalidSampleRate},
		{"negative rate", decode.Waveform{Samples: []float64{0}, SampleRate: -1}, decode.ErrInvalidSampleRate},
		{"nan rate", decode.Waveform{Samples: []float64{0}, SampleRate: math.NaN()}, decode.ErrInvalidSampleRate},
		{"inf start", decode.Waveform{Samples: []float64{0}, SampleRate: 1, StartTime: math.Inf(1)}, decode.ErrInvalidStartTime},
		{"nan sample", decode.Waveform{Samples: []float64{0, math.NaN()}, SampleRate: 1}, decode.ErrNonFiniteSample},
		{"inf sample", decode.Waveform{Samples: []float64{math.Inf(-1)}, SampleRate: 1}, decode.ErrNonFiniteSample},
		{"time overflow", decode.Waveform{Samples: []float64{0, 0}, SampleRate: 1e-308, StartTime: 1e308}, decode.ErrInvalidTimeAxis},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.w.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWaveformTimeAt(t *testing.T) {
	w := decode.Waveform{Samples: []float64{0, 1, 2, 3}, SampleRate: 2e6, StartTime: -1e-6}
	got, err := w.TimeAt(2)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, got, 0, 1e-18, "TimeAt(2)")
	if _, err := w.TimeAt(-1); !errors.Is(err, decode.ErrIndexOutOfRange) {
		t.Fatalf("TimeAt(-1) = %v, want ErrIndexOutOfRange", err)
	}
	if _, err := w.TimeAt(4); !errors.Is(err, decode.ErrIndexOutOfRange) {
		t.Fatalf("TimeAt(4) = %v, want ErrIndexOutOfRange", err)
	}
	approx(t, w.EndTime(), 0.5e-6, 1e-18, "EndTime")
	approx(t, w.Duration(), 1.5e-6, 1e-18, "Duration")

	// SampleValueAt interpolates rather than rounding to a sample index.
	w2 := decode.Waveform{Samples: []float64{0, 10}, SampleRate: 1, StartTime: 0}
	v, err := w2.SampleValueAt(0.25)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, v, 2.5, 1e-12, "SampleValueAt")
}

func TestAlignWaveformsPartialOverlap(t *testing.T) {
	a := decode.Waveform{Samples: []float64{0, 1, 2, 3, 4}, SampleRate: 1, StartTime: 0}
	b := decode.Waveform{Samples: []float64{9, 8, 7, 6, 5, 4}, SampleRate: 1, StartTime: 2}
	ca, cb, diags, err := decode.AlignWaveforms(a, b)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, ca.StartTime, 2, 1e-12, "cropped A start")
	approx(t, ca.EndTime(), 4, 1e-12, "cropped A end")
	approx(t, cb.StartTime, 2, 1e-12, "cropped B start")
	if len(ca.Samples) != 3 || len(cb.Samples) != 3 {
		t.Fatalf("lengths: %d, %d", len(ca.Samples), len(cb.Samples))
	}
	if !hasDiag(diags, decode.CodeWaveformPartialOverlap) {
		t.Fatalf("missing partial-overlap diagnostic: %+v", diags)
	}
}

func TestAlignWaveformsNoOverlap(t *testing.T) {
	a := decode.Waveform{Samples: []float64{0, 1}, SampleRate: 1, StartTime: 0}
	b := decode.Waveform{Samples: []float64{0, 1}, SampleRate: 1, StartTime: 5}
	_, _, _, err := decode.AlignWaveforms(a, b)
	if !errors.Is(err, decode.ErrNoTimeOverlap) {
		t.Fatalf("got %v, want ErrNoTimeOverlap", err)
	}
}

func TestWaveformRejectsNaN(t *testing.T) {
	// Validate must catch non-finite samples before they reach measurements.
	w := decode.Waveform{Samples: []float64{math.NaN()}, SampleRate: 1, StartTime: 0}
	if err := w.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestNewWaveform(t *testing.T) {
	w, err := decode.NewWaveform([]float64{0, 1}, 1e6, -1e-6)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, w.EndTime(), -1e-6+1e-6, 1e-18, "NewWaveform end")
	if _, err := decode.NewWaveform(nil, 1e6, 0); err == nil {
		t.Fatal("expected error for empty samples")
	}
}
