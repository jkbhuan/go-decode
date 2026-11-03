package core

import (
	"fmt"
	"math"
	"sort"
)

// EdgeKind identifies the direction of a threshold crossing.
type EdgeKind string

const (
	EdgeRising  EdgeKind = "rising"
	EdgeFalling EdgeKind = "falling"
)

// Edge is one threshold crossing found in a channel.
//
// With a hysteresis threshold, a rising edge is placed at the crossing of
// threshold.High (V_IH) and a falling edge at the crossing of threshold.Low
// (V_IL). The crossing time is linearly interpolated between the two adjacent
// samples; it is not rounded to a sample index.
type Edge struct {
	Time      float64
	Index     int // sample index just before the crossing
	Kind      EdgeKind
	Threshold float64
	Channel   string
}

// LogicTrace is the result of thresholding one analog channel.
type LogicTrace struct {
	Channel      string
	StartTime    float64
	EndTime      float64
	InitialHigh  bool
	InitialKnown bool
	KnownFrom    float64
	Edges        []Edge
	Diagnostics  []Diagnostic
}

// ExtractLogicTrace converts a waveform into logic levels and interpolated
// edges.
//
// glitchWidth is a duration in seconds. When it is greater than zero, pulses
// shorter than that width are removed as glitches (both of their edges are
// dropped); this filtering is explicit, deterministic and reported in a
// diagnostic. Pass zero to disable it.
//
// The trace never invents an edge at the start of the record: if the record
// begins inside the threshold band, the first level is learned at the first
// sample that clearly belongs to one side of the band, and KnownFrom reports
// when the level became known.
func ExtractLogicTrace(w Waveform, threshold LogicThreshold, glitchWidth float64) (LogicTrace, error) {
	return ExtractLogicTraceOnChannel(w, threshold, glitchWidth, "")
}

// ExtractLogicTraceOnChannel is ExtractLogicTrace with a channel label copied
// onto the trace and every Edge. The label is metadata only.
func ExtractLogicTraceOnChannel(w Waveform, threshold LogicThreshold, glitchWidth float64, channel string) (LogicTrace, error) {
	if err := w.Validate(); err != nil {
		return LogicTrace{}, err
	}
	if err := threshold.Validate(); err != nil {
		return LogicTrace{}, err
	}
	if !IsFinite(glitchWidth) || glitchWidth < 0 {
		return LogicTrace{}, fmt.Errorf("decode: glitch width must be a non-negative finite duration")
	}

	tr := LogicTrace{
		Channel:   channel,
		StartTime: w.StartTime,
		EndTime:   w.EndTime(),
	}

	// Find the first sample that unambiguously defines the level.
	first := -1
	for i, v := range w.Samples {
		if high, known := threshold.classify(float64(v)); known {
			tr.InitialHigh = high
			tr.InitialKnown = true
			first = i
			break
		}
	}
	if first < 0 {
		tr.Diagnostics = append(tr.Diagnostics, NewDiagnostic(
			CodeWaveformNeverCrosses, SeverityWarning, nil,
			"no sample reaches either logic threshold (low %.6g V, high %.6g V); no edges can be determined",
			threshold.Low, threshold.High))
		return tr, nil
	}
	if first > 0 {
		knownAt, _ := w.TimeAt(first)
		tr.KnownFrom = knownAt
		tr.Diagnostics = append(tr.Diagnostics, NewDiagnostic(
			CodeWaveformInitialUnknown, SeverityInfo, timePtr(knownAt),
			"record starts inside the logic threshold band; level is only known from %.12g s", knownAt))
	} else {
		tr.KnownFrom = w.StartTime
	}

	state := tr.InitialHigh
	prevV := float64(w.Samples[first])
	prevT, _ := w.TimeAt(first)
	for i := first + 1; i < len(w.Samples); i++ {
		v := float64(w.Samples[i])
		curT, _ := w.TimeAt(i)
		if state {
			if v <= threshold.Low {
				t := crossingTime(prevT, prevV, curT, v, threshold.Low)
				tr.Edges = append(tr.Edges, Edge{
					Time: t, Index: i - 1, Kind: EdgeFalling, Threshold: threshold.Low, Channel: channel,
				})
				state = false
			}
		} else {
			if v >= threshold.High {
				t := crossingTime(prevT, prevV, curT, v, threshold.High)
				tr.Edges = append(tr.Edges, Edge{
					Time: t, Index: i - 1, Kind: EdgeRising, Threshold: threshold.High, Channel: channel,
				})
				state = true
			}
		}
		prevV = v
		prevT = curT
	}

	if glitchWidth > 0 {
		cleaned, suppressed := suppressGlitches(tr.Edges, glitchWidth)
		if suppressed > 0 {
			tr.Edges = cleaned
			tr.Diagnostics = append(tr.Diagnostics, NewDiagnostic(
				CodeEdgeGlitchSuppressed, SeverityInfo, nil,
				"%d pulse(s) shorter than %.12g s were suppressed by the configured glitch filter",
				suppressed, glitchWidth))
		}
	}

	if len(tr.Edges) == 0 {
		tr.Diagnostics = append(tr.Diagnostics, NewDiagnostic(
			CodeWaveformStatic, SeverityInfo, nil,
			"signal does not cross both logic thresholds; no edges observed"))
		return tr, nil
	}
	// Report under-resolved pulses rather than silently claiming an edge time
	// more precise than the sampling interval supports.
	sampleInterval := 1 / w.SampleRate
	minGap := math.Inf(1)
	for i := 1; i < len(tr.Edges); i++ {
		if gap := tr.Edges[i].Time - tr.Edges[i-1].Time; gap > 0 && gap < minGap {
			minGap = gap
		}
	}
	if minGap < 3*sampleInterval {
		tr.Diagnostics = append(tr.Diagnostics, NewDiagnostic(
			CodeWaveformSamplingLow, SeverityWarning, nil,
			"shortest observed pulse is %.3g samples wide (sample interval %.6g s); edge and pulse timing accuracy is limited by sampling",
			minGap/sampleInterval, sampleInterval))
	}
	return tr, nil
}

// LevelAt returns the logic level at time t. ok is false when the level cannot
// be established (the record starts inside the threshold band and t is before
// the first unambiguous sample).
func (tr LogicTrace) LevelAt(t float64) (high, ok bool) {
	if !tr.InitialKnown || t < tr.KnownFrom {
		return false, false
	}
	if len(tr.Edges) == 0 || t < tr.Edges[0].Time {
		return tr.InitialHigh, true
	}
	// Find the last edge at or before t.
	i := sort.Search(len(tr.Edges), func(i int) bool { return tr.Edges[i].Time > t }) - 1
	if i < 0 {
		return tr.InitialHigh, true
	}
	return tr.Edges[i].Kind == EdgeRising, true
}

// EdgeAt returns the edge with the given index and whether it was found.
func (tr LogicTrace) EdgeAt(i int) (Edge, bool) {
	if i < 0 || i >= len(tr.Edges) {
		return Edge{}, false
	}
	return tr.Edges[i], true
}

// EdgesOfKind returns all edges of one kind in time order.
func (tr LogicTrace) EdgesOfKind(kind EdgeKind) []Edge {
	out := make([]Edge, 0, len(tr.Edges)/2+1)
	for _, e := range tr.Edges {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func crossingTime(t0, v0, t1, v1, level float64) float64 {
	if t1 <= t0 {
		return t1
	}
	den := v1 - v0
	if den == 0 || math.IsNaN(den) {
		return t1
	}
	f := (level - v0) / den
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	return t0 + f*(t1-t0)
}

// suppressGlitches removes pulses narrower than width. A pulse is a pair of
// consecutive edges. When a pulse is removed, both edges disappear so the
// surrounding level continues unchanged; this is the conventional way to drop
// a noise spike without moving the surviving edges.
func suppressGlitches(edges []Edge, width float64) ([]Edge, int) {
	if width <= 0 || len(edges) < 2 {
		return edges, 0
	}
	out := make([]Edge, 0, len(edges))
	suppressed := 0
	for _, e := range edges {
		if len(out) > 0 && e.Time-out[len(out)-1].Time < width {
			out = out[:len(out)-1]
			suppressed++
			continue
		}
		out = append(out, e)
	}
	return out, suppressed
}
