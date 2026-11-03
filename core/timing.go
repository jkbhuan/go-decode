package core

import "sort"

// TimingName is a stable identifier for a measured timing parameter.
type TimingName string

// I2C timing names. The definitions and reference levels are based on NXP
// UM10204 Rev. 7 (2021-10-01), Figure 38 and Table 11 (F/S-mode devices).
const (
	TimingHDSTA TimingName = "tHD;STA"
	TimingSUSTA TimingName = "tSU;STA"
	TimingSUSTO TimingName = "tSU;STO"
	TimingBUF   TimingName = "tBUF"
	TimingSUDAT TimingName = "tSU;DAT"
	TimingHDDAT TimingName = "tHD;DAT"
	TimingTHIGH TimingName = "tHIGH"
	TimingTLOW  TimingName = "tLOW"
)

// SMI / MDIO timing names.
//
// tSU(MDIO) and tHD(MDIO) are measured relative to the MDC rising edge for
// both directions:
//   - Write frames: the STA drives MDIO, so the definitions follow IEEE
//     802.3-2022 Clause 22.3.4 and Figure 22-18.
//   - Read frames: the PHY drives MDIO from the second turnaround bit onward,
//     so the same tSU/tHD names are reported with Direction=DirectionRead;
//     hold corresponds to the PHY clock-to-output delay of Figure 22-19.
const (
	TimingMDCPeriod TimingName = "MDCPeriod"
	TimingMDCHigh   TimingName = "MDCHigh"
	TimingMDCLow    TimingName = "MDCLow"
	TimingSUMDIO    TimingName = "tSU(MDIO)"
	TimingHDMDIO    TimingName = "tHD(MDIO)"
)

// Direction is the data transfer direction of a frame or timing candidate.
type Direction uint8

const (
	DirectionUnknown Direction = iota
	DirectionWrite
	DirectionRead
)

// String implements fmt.Stringer.
func (d Direction) String() string {
	switch d {
	case DirectionWrite:
		return "write"
	case DirectionRead:
		return "read"
	default:
		return "unknown"
	}
}

// Timing is one valid, observed measurement.
//
// Value, X1 and X2 are in seconds on the same instrument horizontal time axis
// as Waveform.StartTime. X1 <= X2 and Value == X2-X1 (up to floating-point
// round-off). A Value of zero is a legitimate measurement.
//
// The timing is only present when the measurement could actually be observed;
// a missing measurement is represented by the absence of a Timing plus a
// Diagnostic explaining why, never by a fabricated zero or NaN.
type Timing struct {
	Name             TimingName
	Value            float64
	X1               float64
	X2               float64
	TransactionIndex int // I2C transaction; -1 when not applicable
	FrameIndex       int
	ByteIndex        int
	BitIndex         int
	Phase            string
	Direction        Direction
}

// SelectionMode chooses among multiple valid timing candidates.
type SelectionMode uint8

const (
	SelectFirst SelectionMode = iota
	SelectMin
	SelectMax
)

// TimingFilter filters timing candidates before selection. A zero Name,
// DirectionUnknown, nil index pointers and an empty Phase mean "do not
// filter by that field".
type TimingFilter struct {
	Name       TimingName
	Direction  Direction
	FrameIndex *int
	Phase      string
	ByteIndex  *int
	BitIndex   *int
}

func (f TimingFilter) matches(t Timing) bool {
	if f.Name != "" && t.Name != f.Name {
		return false
	}
	if f.Direction != DirectionUnknown && t.Direction != f.Direction {
		return false
	}
	if f.FrameIndex != nil && t.FrameIndex != *f.FrameIndex {
		return false
	}
	if f.ByteIndex != nil && t.ByteIndex != *f.ByteIndex {
		return false
	}
	if f.BitIndex != nil && t.BitIndex != *f.BitIndex {
		return false
	}
	if f.Phase != "" && t.Phase != f.Phase {
		return false
	}
	return true
}

// SelectTiming first filters all candidates, then selects one.
//
// It returns false when no candidate matches; it never fabricates an empty
// timing. Selection is deterministic: ties are resolved by the earliest X1,
// and remaining ties by the original slice order.
func SelectTiming(all []Timing, filter TimingFilter, mode SelectionMode) (Timing, bool) {
	candidates := make([]Timing, 0, len(all))
	for _, t := range all {
		if filter.matches(t) {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return Timing{}, false
	}
	switch mode {
	case SelectMin:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Value != candidates[j].Value {
				return candidates[i].Value < candidates[j].Value
			}
			return candidates[i].X1 < candidates[j].X1
		})
	case SelectMax:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Value != candidates[j].Value {
				return candidates[i].Value > candidates[j].Value
			}
			return candidates[i].X1 < candidates[j].X1
		})
	default: // SelectFirst and unknown modes
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].X1 != candidates[j].X1 {
				return candidates[i].X1 < candidates[j].X1
			}
			return candidates[i].Value < candidates[j].Value
		})
	}
	return candidates[0], true
}

// SelectTimings returns all matching candidates in original order. It is
// useful when the caller wants to inspect every measurement rather than a
// single representative value.
func SelectTimings(all []Timing, filter TimingFilter) []Timing {
	out := make([]Timing, 0, len(all))
	for _, t := range all {
		if filter.matches(t) {
			out = append(out, t)
		}
	}
	return out
}

// FrequencyMeasurement is a derived frequency (Hz) with the interval on which
// it was based. It is kept separate from Timing because Timing.Value is
// strictly a duration in seconds.
type FrequencyMeasurement struct {
	TransactionIndex int // I2C transaction
	Value            float64
	X1               float64
	X2               float64
}
