package core

import "fmt"

// LogicThreshold defines the voltage thresholds used to turn one analog
// channel into a logic level.
//
// Low is the voltage at or below which the signal is unambiguously LOW (the
// V_IL threshold). High is the voltage at or above which it is unambiguously
// HIGH (the V_IH threshold). When High > Low the gap acts as hysteresis, which
// suppresses noise around a single decision threshold. When High == Low the
// threshold behaves as a single comparator level.
//
// These thresholds are used for protocol decoding. Timing measurements use a
// separate threshold (see I2COptions.TimingThreshold / SMIOptions.TimingThreshold)
// because standards define their timing reference levels independently.
type LogicThreshold struct {
	Low  float64
	High float64
}

// SingleThreshold returns a threshold with no hysteresis.
func SingleThreshold(v float64) LogicThreshold {
	return LogicThreshold{Low: v, High: v}
}

// Validate reports whether the threshold is usable.
func (t LogicThreshold) Validate() error {
	if !IsFinite(t.Low) || !IsFinite(t.High) {
		return fmt.Errorf("decode: logic threshold must be finite (low=%g, high=%g)", t.Low, t.High)
	}
	if t.High < t.Low {
		return fmt.Errorf("decode: logic threshold high (%g V) must be >= low (%g V)", t.High, t.Low)
	}
	return nil
}

// classify maps a voltage to a definite logic level. The bool result is false
// while the voltage is inside the indeterminate band.
func (t LogicThreshold) classify(v float64) (high, known bool) {
	switch {
	case v >= t.High:
		return true, true
	case v <= t.Low:
		return false, true
	default:
		return false, false
	}
}
