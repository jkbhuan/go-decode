package decode_test

import (
	"math"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

const (
	testVDD    = 3.3
	testHigh   = 3.3
	testLow    = 0.0
	testLogic  = 1.65
	i2cTimingL = 0.3 * testVDD // 0.99 V
	i2cTimingH = 0.7 * testVDD // 2.31 V
)

func defaultWaveConfig(start, rate, duration float64) testwave.Config {
	return testwave.Config{
		StartTime:  start,
		SampleRate: rate,
		Duration:   duration,
		Low:        testLow,
		High:       testHigh,
	}
}

func baseI2COptions() decode.I2COptions {
	return decode.I2COptions{
		SCLThreshold: decode.SingleThreshold(testLogic),
		SDAThreshold: decode.SingleThreshold(testLogic),
		VDD:          testVDD,
	}
}

func baseSMIOptions() decode.SMIOptions {
	return decode.SMIOptions{
		MDCThreshold:    decode.SingleThreshold(testLogic),
		MDIOThreshold:   decode.SingleThreshold(testLogic),
		TimingThreshold: &decode.LogicThreshold{Low: 0.8, High: 2.0},
	}
}

func mustI2CBuilder(t testing.TB, cfg testwave.I2CConfig) *testwave.I2CBuilder {
	t.Helper()
	b, err := testwave.NewI2C(cfg)
	if err != nil {
		t.Fatalf("NewI2C: %v", err)
	}
	return b
}

func mustMDIOBuilder(t testing.TB, cfg testwave.MDIOConfig) *testwave.MDIOBuilder {
	t.Helper()
	b, err := testwave.NewMDIO(cfg)
	if err != nil {
		t.Fatalf("NewMDIO: %v", err)
	}
	return b
}

func buildI2C(t *testing.T, b *testwave.I2CBuilder) (decode.Waveform, decode.Waveform) {
	t.Helper()
	scl, sda, err := b.Build()
	if err != nil {
		t.Fatalf("I2C build: %v", err)
	}
	return scl, sda
}

func buildMDIO(t *testing.T, b *testwave.MDIOBuilder) (decode.Waveform, decode.Waveform) {
	t.Helper()
	mdc, mdio, err := b.Build()
	if err != nil {
		t.Fatalf("MDIO build: %v", err)
	}
	return mdc, mdio
}

func approx(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.12g, want %.12g +/- %g", what, got, want, tol)
	}
}

func findTiming(ts []decode.Timing, name decode.TimingName) []decode.Timing {
	out := make([]decode.Timing, 0)
	for _, tm := range ts {
		if tm.Name == name {
			out = append(out, tm)
		}
	}
	return out
}

func hasDiag(ds []decode.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func checkTimingInvariant(t *testing.T, tm decode.Timing) {
	t.Helper()
	if tm.X2 < tm.X1 {
		t.Errorf("%s: X2 (%g) < X1 (%g)", tm.Name, tm.X2, tm.X1)
	}
	if math.Abs(tm.Value-(tm.X2-tm.X1)) > 1e-15 {
		t.Errorf("%s: Value=%g, X2-X1=%g", tm.Name, tm.Value, tm.X2-tm.X1)
	}
	if math.IsNaN(tm.Value) || math.IsInf(tm.Value, 0) {
		t.Errorf("%s: non-finite value", tm.Name)
	}
}
