package core

import (
	"errors"
	"fmt"
	"math"
)

// Waveform is one channel of a digitized analog voltage record.
//
// Units:
//   - Samples are volts (V).
//   - SampleRate is samples per second (Sa/s).
//   - StartTime is seconds (s) on the oscilloscope's horizontal time base and
//     refers to the time of Samples[0]. It must never be a Unix epoch value.
//
// The sample time of index i is StartTime + i/SampleRate. Two channels may have
// different sample rates, lengths and start times; they are aligned by time,
// never by array index.
type Waveform struct {
	Samples    []float64
	SampleRate float64
	StartTime  float64
}

// Errors returned for invalid waveforms and channel alignment.
var (
	ErrEmptyWaveform       = errors.New("decode: waveform has no samples")
	ErrInvalidSampleRate   = errors.New("decode: sample rate must be a positive finite value")
	ErrInvalidStartTime    = errors.New("decode: start time must be finite")
	ErrNonFiniteSample     = errors.New("decode: sample value is not finite")
	ErrInvalidTimeAxis     = errors.New("decode: waveform time range is not finite")
	ErrIndexOutOfRange     = errors.New("decode: sample index out of range")
	ErrNoTimeOverlap       = errors.New("decode: channels have no overlapping time range")
	ErrInsufficientOverlap = errors.New("decode: channel overlap contains fewer than two samples")
)

// NewWaveform validates the arguments and returns a Waveform.
func NewWaveform(samples []float64, sampleRate, startTime float64) (Waveform, error) {
	w := Waveform{Samples: samples, SampleRate: sampleRate, StartTime: startTime}
	if err := w.Validate(); err != nil {
		return Waveform{}, err
	}
	return w, nil
}

// Validate checks array length, sample rate, start time and all sample values.
func (w Waveform) Validate() error {
	if len(w.Samples) == 0 {
		return ErrEmptyWaveform
	}
	if !IsFinite(w.SampleRate) || w.SampleRate <= 0 {
		return ErrInvalidSampleRate
	}
	if !IsFinite(w.StartTime) {
		return ErrInvalidStartTime
	}
	// Guard against overflow of StartTime + (N-1)/SampleRate, which would
	// produce infinite edge and cursor coordinates.
	if end := w.StartTime + float64(len(w.Samples)-1)/w.SampleRate; !IsFinite(end) {
		return ErrInvalidTimeAxis
	}
	for i, v := range w.Samples {
		if !IsFinite(v) {
			return fmt.Errorf("%w (index %d)", ErrNonFiniteSample, i)
		}
	}
	return nil
}

// TimeAt returns the instrument time in seconds for sample index i.
func (w Waveform) TimeAt(i int) (float64, error) {
	if i < 0 || i >= len(w.Samples) {
		return 0, fmt.Errorf("%w: index %d, length %d", ErrIndexOutOfRange, i, len(w.Samples))
	}
	return w.StartTime + float64(i)/w.SampleRate, nil
}

// EndTime is the time of the last sample, in seconds.
func (w Waveform) EndTime() float64 {
	if len(w.Samples) == 0 {
		return w.StartTime
	}
	return w.StartTime + float64(len(w.Samples)-1)/w.SampleRate
}

// Duration is the time span between the first and last sample, in seconds.
func (w Waveform) Duration() float64 {
	if len(w.Samples) < 2 {
		return 0
	}
	return float64(len(w.Samples)-1) / w.SampleRate
}

// SampleValueAt returns the linearly interpolated voltage at time t. Times
// outside the record return an error. The interpolation is only a numerical
// convenience; callers must not claim more accuracy than the sample interval
// supports.
func (w Waveform) SampleValueAt(t float64) (float64, error) {
	if len(w.Samples) == 0 {
		return 0, ErrEmptyWaveform
	}
	if !IsFinite(t) {
		return 0, fmt.Errorf("decode: time is not finite")
	}
	if !IsFinite(w.SampleRate) || w.SampleRate <= 0 || !IsFinite(w.StartTime) {
		return 0, fmt.Errorf("decode: waveform is invalid")
	}
	pos := (t - w.StartTime) * w.SampleRate
	if pos < -1e-9 || pos > float64(len(w.Samples)-1)+1e-9 {
		return 0, fmt.Errorf("decode: time %g is outside waveform record [%g, %g]", t, w.StartTime, w.EndTime())
	}
	if pos <= 0 {
		return w.Samples[0], nil
	}
	last := len(w.Samples) - 1
	if pos >= float64(last) {
		return w.Samples[last], nil
	}
	i := int(math.Floor(pos))
	f := pos - float64(i)
	return w.Samples[i]*(1-f) + w.Samples[i+1]*f, nil
}

// AlignWaveforms crops two channels to the common time range so that every
// returned sample exists on both channels. It never resamples. When only part
// of the records overlap, a waveform_partial_overlap diagnostic reports the
// retained interval. If there is no overlap an error is returned.
//
// The returned waveforms keep the original absolute instrument time axis; the
// first retained sample becomes the new StartTime.
func AlignWaveforms(a, b Waveform) (Waveform, Waveform, []Diagnostic, error) {
	if err := a.Validate(); err != nil {
		return Waveform{}, Waveform{}, nil, fmt.Errorf("decode: channel A: %w", err)
	}
	if err := b.Validate(); err != nil {
		return Waveform{}, Waveform{}, nil, fmt.Errorf("decode: channel B: %w", err)
	}

	lo := math.Max(a.StartTime, b.StartTime)
	hi := math.Min(a.EndTime(), b.EndTime())
	if hi < lo {
		return Waveform{}, Waveform{}, nil, ErrNoTimeOverlap
	}
	// A tiny tolerance absorbs floating-point round-off when the two records
	// share an endpoint.
	const eps = 1e-12
	if hi-lo < -eps {
		return Waveform{}, Waveform{}, nil, ErrNoTimeOverlap
	}

	ca, err := cropToRange(a, lo, hi)
	if err != nil {
		return Waveform{}, Waveform{}, nil, fmt.Errorf("decode: channel A: %w", err)
	}
	cb, err := cropToRange(b, lo, hi)
	if err != nil {
		return Waveform{}, Waveform{}, nil, fmt.Errorf("decode: channel B: %w", err)
	}
	if len(ca.Samples) < 2 || len(cb.Samples) < 2 {
		return Waveform{}, Waveform{}, nil, ErrInsufficientOverlap
	}

	var diags []Diagnostic
	if ca.StartTime > a.StartTime+eps || ca.EndTime() < a.EndTime()-eps ||
		cb.StartTime > b.StartTime+eps || cb.EndTime() < b.EndTime()-eps {
		caStart, cbStart := ca.StartTime, cb.StartTime
		diags = append(diags, NewDiagnostic(
			CodeWaveformPartialOverlap, SeverityWarning, timePtr(math.Max(caStart, cbStart)),
			"channels only partially overlap; analysis is limited to [%.12g s, %.12g s]",
			math.Max(caStart, cbStart), math.Min(ca.EndTime(), cb.EndTime())))
	}
	return ca, cb, diags, nil
}

func cropToRange(w Waveform, lo, hi float64) (Waveform, error) {
	const eps = 1e-12
	n := len(w.Samples)
	startIdx := int(math.Ceil((lo-w.StartTime)*w.SampleRate - eps))
	endIdx := int(math.Floor((hi-w.StartTime)*w.SampleRate + eps))
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > n-1 {
		endIdx = n - 1
	}
	if startIdx > endIdx || startIdx >= n || endIdx < 0 {
		return Waveform{}, ErrNoTimeOverlap
	}
	out := Waveform{
		Samples:    w.Samples[startIdx : endIdx+1],
		SampleRate: w.SampleRate,
		StartTime:  w.StartTime + float64(startIdx)/w.SampleRate,
	}
	return out, nil
}

func IsFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
