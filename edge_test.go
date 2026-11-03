package decode_test

import (
	"math"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

func TestExtractLogicTraceIdealStep(t *testing.T) {
	cfg := defaultWaveConfig(-2e-6, 100e6, 10e-6)
	trans := []testwave.Transition{
		{T: -1e-6, High: true},
		{T: 2e-6, High: false},
		{T: 4e-6, High: true},
	}
	w, err := testwave.Build(cfg, false, trans)
	if err != nil {
		t.Fatal(err)
	}
	th := decode.LogicThreshold{Low: 0.99, High: 2.31}
	tr, err := decode.ExtractLogicTrace(w, th, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.InitialKnown || tr.InitialHigh {
		t.Fatalf("initial state: known=%v high=%v", tr.InitialKnown, tr.InitialHigh)
	}
	if len(tr.Edges) != 3 {
		t.Fatalf("got %d edges, want 3: %+v", len(tr.Edges), tr.Edges)
	}
	tol := 1.0 / cfg.SampleRate
	kinds := []decode.EdgeKind{decode.EdgeRising, decode.EdgeFalling, decode.EdgeRising}
	times := []float64{-1e-6, 2e-6, 4e-6}
	for i, e := range tr.Edges {
		if e.Kind != kinds[i] {
			t.Errorf("edge %d kind %s, want %s", i, e.Kind, kinds[i])
		}
		approx(t, e.Time, times[i], tol, "edge time")
	}
	// LevelAt uses the same interpolated edge times.
	high, ok := tr.LevelAt(1e-6)
	if !ok || !high {
		t.Fatalf("LevelAt(1e-6) = %v,%v", high, ok)
	}
	high, ok = tr.LevelAt(3e-6)
	if !ok || high {
		t.Fatalf("LevelAt(3e-6) = %v,%v", high, ok)
	}
}

func TestExtractLogicTraceLinearRampInterpolation(t *testing.T) {
	// A 1 us linear ramp centred at 5 us. With thresholds 0.3/0.7 VDD the
	// rising crossing must be at 5 us + 0.2*1 us = 5.2 us, and the falling
	// crossing at 10 us + 0.2*1 us = 10.2 us.
	cfg := defaultWaveConfig(0, 200e6, 15e-6)
	cfg.RiseTime = 1e-6
	cfg.FallTime = 1e-6
	w, err := testwave.Build(cfg, false, []testwave.Transition{
		{T: 5e-6, High: true},
		{T: 10e-6, High: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := decode.ExtractLogicTrace(w, decode.LogicThreshold{Low: 0.99, High: 2.31}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Edges) != 2 {
		t.Fatalf("got %d edges, want 2: %+v", len(tr.Edges), tr.Edges)
	}
	tol := 2.0 / cfg.SampleRate
	approx(t, tr.Edges[0].Time, 5.2e-6, tol, "70% rising crossing")
	approx(t, tr.Edges[1].Time, 10.2e-6, tol, "30% falling crossing")
	if tr.Edges[0].Threshold != 2.31 || tr.Edges[1].Threshold != 0.99 {
		t.Fatalf("edge thresholds: %v, %v", tr.Edges[0].Threshold, tr.Edges[1].Threshold)
	}
}

func TestExtractLogicTraceHysteresisRejectsNoiseBand(t *testing.T) {
	// Small oscillation around 1.65 V, entirely inside the 1.2/2.1 V band.
	n := 1000
	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(1.65 + 0.2*math.Sin(float64(i)*0.3))
	}
	w := decode.Waveform{Samples: samples, SampleRate: 1e6, StartTime: 0}
	tr, err := decode.ExtractLogicTrace(w, decode.LogicThreshold{Low: 1.2, High: 2.1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tr.InitialKnown {
		t.Fatal("level should be unknown inside the hysteresis band")
	}
	if len(tr.Edges) != 0 {
		t.Fatalf("invented edges: %+v", tr.Edges)
	}
	if !hasDiag(tr.Diagnostics, decode.CodeWaveformNeverCrosses) {
		t.Fatalf("missing never-crosses diagnostic: %+v", tr.Diagnostics)
	}
}

func TestExtractLogicTraceInitialUnknownNoInventedEdge(t *testing.T) {
	w := decode.Waveform{
		Samples:    []float32{1.65, 1.65, 3.3, 3.3, 0.0, 0.0},
		SampleRate: 1e6,
		StartTime:  100e-6,
	}
	tr, err := decode.ExtractLogicTrace(w, decode.LogicThreshold{Low: 1.2, High: 2.1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.InitialKnown || !tr.InitialHigh {
		t.Fatalf("initial: known=%v high=%v", tr.InitialKnown, tr.InitialHigh)
	}
	approx(t, tr.KnownFrom, 102e-6, 1e-18, "KnownFrom")
	if len(tr.Edges) != 1 || tr.Edges[0].Kind != decode.EdgeFalling {
		t.Fatalf("expected only the falling edge, got %+v", tr.Edges)
	}
	if !hasDiag(tr.Diagnostics, decode.CodeWaveformInitialUnknown) {
		t.Fatalf("missing initial-unknown diagnostic: %+v", tr.Diagnostics)
	}
}

func TestGlitchSuppression(t *testing.T) {
	cfg := defaultWaveConfig(0, 200e6, 10e-6)
	w, err := testwave.Build(cfg, false, []testwave.Transition{
		{T: 1e-6, High: true},
		{T: 1.05e-6, High: false}, // 50 ns glitch
		{T: 2e-6, High: true},
		{T: 3e-6, High: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	th := decode.SingleThreshold(1.65)

	raw, err := decode.ExtractLogicTrace(w, th, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Edges) != 4 {
		t.Fatalf("raw edges = %d, want 4", len(raw.Edges))
	}

	filtered, err := decode.ExtractLogicTrace(w, th, 200e-9)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Edges) != 2 {
		t.Fatalf("filtered edges = %d, want 2: %+v", len(filtered.Edges), filtered.Edges)
	}
	approx(t, filtered.Edges[0].Time, 2e-6, 1.0/cfg.SampleRate, "surviving rising edge")
	approx(t, filtered.Edges[1].Time, 3e-6, 1.0/cfg.SampleRate, "surviving falling edge")
	if !hasDiag(filtered.Diagnostics, decode.CodeEdgeGlitchSuppressed) {
		t.Fatalf("missing glitch diagnostic: %+v", filtered.Diagnostics)
	}
	if high, ok := filtered.LevelAt(1.5e-6); !ok || high {
		t.Fatalf("glitch pulse changed the level: high=%v ok=%v", high, ok)
	}
}

func TestExtractLogicTraceRejectsBadInput(t *testing.T) {
	w := decode.Waveform{Samples: []float32{0, 1}, SampleRate: 1, StartTime: 0}
	if _, err := decode.ExtractLogicTrace(w, decode.LogicThreshold{Low: 2, High: 1}, 0); err == nil {
		t.Fatal("expected invalid threshold error")
	}
	if _, err := decode.ExtractLogicTrace(w, decode.SingleThreshold(0.5), -1); err == nil {
		t.Fatal("expected negative glitch width error")
	}
}

func TestExtractLogicTraceLowSamplingDiagnostic(t *testing.T) {
	samples := []float32{0, 0, 0, 3.3, 0, 0, 0}
	w := decode.Waveform{Samples: samples, SampleRate: 1e6, StartTime: 0}
	tr, err := decode.ExtractLogicTrace(w, decode.SingleThreshold(1.65), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Edges) != 2 {
		t.Fatalf("edges = %d", len(tr.Edges))
	}
	if !hasDiag(tr.Diagnostics, decode.CodeWaveformSamplingLow) {
		t.Fatalf("missing low-sampling diagnostic: %+v", tr.Diagnostics)
	}
}

func TestExtractLogicTraceChannelLabel(t *testing.T) {
	w := decode.Waveform{Samples: []float32{0, 3.3, 0}, SampleRate: 1e6, StartTime: 0}
	tr, err := decode.ExtractLogicTraceOnChannel(w, decode.SingleThreshold(1.65), 0, "SCL")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Channel != "SCL" {
		t.Fatalf("trace channel = %q", tr.Channel)
	}
	for _, e := range tr.Edges {
		if e.Channel != "SCL" {
			t.Fatalf("edge channel = %q", e.Channel)
		}
	}
}

func TestEdgeAccessors(t *testing.T) {
	w := decode.Waveform{Samples: []float32{0, 3.3, 0, 0}, SampleRate: 1e6, StartTime: 0}
	tr, err := decode.ExtractLogicTrace(w, decode.SingleThreshold(1.65), 0)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := tr.EdgeAt(0); !ok || e.Kind != decode.EdgeRising {
		t.Fatalf("EdgeAt(0) = %+v, %v", e, ok)
	}
	if _, ok := tr.EdgeAt(-1); ok {
		t.Fatal("EdgeAt(-1) should fail")
	}
	if got := tr.EdgesOfKind(decode.EdgeFalling); len(got) != 1 {
		t.Fatalf("falling edges = %d", len(got))
	}
	if got := tr.EdgesOfKind(decode.EdgeRising); len(got) != 1 {
		t.Fatalf("rising edges = %d", len(got))
	}
}
