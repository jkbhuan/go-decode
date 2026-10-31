package testwave

import (
	"fmt"

	"github.com/jkbhuan/go-decode/core"
)

// MDIOConfig controls a Clause 22 MDC/MDIO waveform.
//
// MDC has MDCHighTime high and MDCPeriod-MDCHighTime low. MDIO changes
// DataHold after the MDC falling edge that starts a bit; the following rising
// edge is (MDCPeriod-MDCHighTime)-DataHold later, which is the observable
// setup time.
type MDIOConfig struct {
	Wave        Config
	MDCPeriod   float64
	MDCHighTime float64
	DataSetup   float64
	DataHold    float64
}

func (c MDIOConfig) lowTime() float64 {
	return c.MDCPeriod - c.MDCHighTime
}

// MDIOBuilder schedules MDC/MDIO transitions.
type MDIOBuilder struct {
	cfg MDIOConfig

	start    float64
	end      float64
	started  bool
	mdcHigh  bool
	mdioHigh bool

	mdc  []Transition
	mdio []Transition
}

// NewMDIO creates an MDIO waveform builder.
func NewMDIO(cfg MDIOConfig) (*MDIOBuilder, error) {
	if err := cfg.Wave.Validate(); err != nil {
		return nil, err
	}
	if cfg.MDCPeriod <= 0 || cfg.MDCHighTime <= 0 || cfg.MDCHighTime >= cfg.MDCPeriod {
		return nil, fmt.Errorf("testwave: MDC high time must be in (0, period)")
	}
	if cfg.DataHold < 0 || cfg.DataSetup < 0 {
		return nil, fmt.Errorf("testwave: MDIO setup/hold must be non-negative")
	}
	if cfg.DataHold+cfg.DataSetup > cfg.lowTime()+1e-15 {
		return nil, fmt.Errorf("testwave: MDIO hold+setup (%g) exceeds MDC low time (%g)",
			cfg.DataHold+cfg.DataSetup, cfg.lowTime())
	}
	return &MDIOBuilder{
		cfg:      cfg,
		start:    cfg.Wave.StartTime,
		end:      cfg.Wave.StartTime,
		mdcHigh:  true,
		mdioHigh: true,
	}, nil
}

// Idle advances time, returning MDIO to its pulled-up high level.
func (b *MDIOBuilder) Idle(d float64) {
	if !b.mdioHigh {
		b.setMDIO(true, b.end)
	}
	if d > 0 {
		b.end += d
	}
}

// Bit emits one bit on the next MDC cycle.
func (b *MDIOBuilder) Bit(high bool) {
	if !b.started {
		b.setMDC(false, b.end)
		b.started = true
	}
	cycle := b.end
	if high != b.mdioHigh {
		b.setMDIO(high, cycle+b.cfg.DataHold)
	}
	b.setMDC(true, cycle+b.cfg.lowTime())
	b.setMDC(false, cycle+b.cfg.MDCPeriod)
	b.end = cycle + b.cfg.MDCPeriod
}

// Bits emits a raw bit sequence (MSB-first order is the caller's
// responsibility).
func (b *MDIOBuilder) Bits(bits []bool) {
	for _, bit := range bits {
		b.Bit(bit)
	}
}

// Preamble emits n logic-one bits.
func (b *MDIOBuilder) Preamble(n int) {
	for i := 0; i < n; i++ {
		b.Bit(true)
	}
}

// Frame emits a complete Clause 22 frame without preamble: ST, OP, PHYAD,
// REGAD, TA and DATA. Read and write both use TA bits 1,0 (for a read this is
// the waveform-visible Z (pull-up) followed by the PHY-driven zero).
func (b *MDIOBuilder) Frame(dir core.Direction, phy, reg uint8, data uint16) {
	bits := FrameBits(dir, phy, reg, data)
	b.Bits(bits)
}

// FrameBits returns the 32 frame bits (ST..DATA) for a Clause 22 frame.
func FrameBits(dir core.Direction, phy, reg uint8, data uint16) []bool {
	bits := []bool{false, true} // ST = 01
	switch dir {
	case core.DirectionWrite:
		bits = append(bits, false, true) // OP = 01
	default:
		bits = append(bits, true, false) // OP = 10
	}
	bits = append(bits, BitsMSB(uint32(phy&0x1F), 5)...)
	bits = append(bits, BitsMSB(uint32(reg&0x1F), 5)...)
	bits = append(bits, true, false) // TA = 10 / Z0
	bits = append(bits, BitsMSB(uint32(data), 16)...)
	return bits
}

// BitsMSB returns the n least-significant bits of v, MSB first.
func BitsMSB(v uint32, n int) []bool {
	bits := make([]bool, n)
	for i := 0; i < n; i++ {
		bits[i] = (v>>uint(n-1-i))&1 != 0
	}
	return bits
}

// EndTime returns the time of the last scheduled transition.
func (b *MDIOBuilder) EndTime() float64 { return b.end }

// Transitions returns copies of the planned MDC and MDIO transitions.
func (b *MDIOBuilder) Transitions() (mdc, mdio []Transition) {
	return append([]Transition(nil), b.mdc...), append([]Transition(nil), b.mdio...)
}

// Build synthesizes the MDC and MDIO waveforms.
func (b *MDIOBuilder) Build() (core.Waveform, core.Waveform, error) {
	cfg := b.cfg.Wave
	if cfg.Duration <= 0 {
		// End after the final MDC falling edge. Adding whole extra MDC cycles
		// would let the decoder sample idle bits as if they were part of the
		// frame.
		cfg.Duration = b.end - cfg.StartTime
	}
	mdc, err := Build(cfg, true, b.mdc)
	if err != nil {
		return core.Waveform{}, core.Waveform{}, err
	}
	mdio, err := Build(cfg, true, b.mdio)
	if err != nil {
		return core.Waveform{}, core.Waveform{}, err
	}
	return mdc, mdio, nil
}

func (b *MDIOBuilder) setMDC(high bool, at float64) {
	if high == b.mdcHigh {
		if at > b.end {
			b.end = at
		}
		return
	}
	if at < b.end {
		at = b.end
	}
	b.mdc = append(b.mdc, Transition{T: at, High: high})
	b.mdcHigh = high
	if at > b.end {
		b.end = at
	}
}

func (b *MDIOBuilder) setMDIO(high bool, at float64) {
	if high == b.mdioHigh {
		if at > b.end {
			b.end = at
		}
		return
	}
	if at < b.end {
		at = b.end
	}
	b.mdio = append(b.mdio, Transition{T: at, High: high})
	b.mdioHigh = high
	if at > b.end {
		b.end = at
	}
}
