package testwave

import (
	"fmt"

	"github.com/jkbhuan/go-decode/core"
)

// I2CConfig controls an I2C bus waveform.
//
// SCL has a low time SCLLowTime and a high time SCLHighTime. A data bit changes
// DataHold after an SCL falling edge and the next SCL rising edge is
// SCLLowTime later, so the observable setup time is SCLLowTime-DataHold.
// Set SCLLowTime == DataSetup+DataHold for exact setup/hold test vectors.
type I2CConfig struct {
	Wave        Config
	SCLHighTime float64
	SCLLowTime  float64
	DataSetup   float64
	DataHold    float64
	StartHold   float64 // tHD;STA in the generated waveform
	StartSetup  float64 // tSU;STA for a repeated START
	StopSetup   float64 // tSU;STO
}

// I2CBuilder schedules SCL/SDA transitions.
type I2CBuilder struct {
	cfg I2CConfig

	start   float64
	end     float64
	lowAt   float64
	sclHigh bool
	sdaHigh bool
	stretch float64
	err     error

	scl []Transition
	sda []Transition
}

// NewI2C creates an I2C waveform builder.
func NewI2C(cfg I2CConfig) (*I2CBuilder, error) {
	if err := cfg.Wave.Validate(); err != nil {
		return nil, err
	}
	if cfg.SCLHighTime <= 0 || cfg.SCLLowTime <= 0 {
		return nil, fmt.Errorf("testwave: I2C SCL high/low times must be positive")
	}
	if cfg.DataHold < 0 || cfg.DataSetup < 0 || cfg.StartHold < 0 || cfg.StartSetup < 0 || cfg.StopSetup < 0 {
		return nil, fmt.Errorf("testwave: I2C timing values must be non-negative")
	}
	if cfg.DataHold+cfg.DataSetup > cfg.SCLLowTime+1e-15 {
		return nil, fmt.Errorf("testwave: I2C data hold+setup (%g) exceeds SCL low time (%g)",
			cfg.DataHold+cfg.DataSetup, cfg.SCLLowTime)
	}
	b := &I2CBuilder{
		cfg:     cfg,
		start:   cfg.Wave.StartTime,
		end:     cfg.Wave.StartTime,
		sclHigh: true,
		sdaHigh: true,
	}
	return b, nil
}

// Idle advances time with SCL and SDA unchanged.
func (b *I2CBuilder) Idle(d float64) {
	if d > 0 {
		b.end += d
	}
}

// StretchLow extends the next SCL low period by d (clock stretching).
func (b *I2CBuilder) StretchLow(d float64) {
	if d > 0 {
		b.stretch += d
	}
}

// Start emits a START condition followed by the first SCL falling edge.
func (b *I2CBuilder) Start() {
	t0 := b.end
	b.setSDA(false, t0)
	fall := t0 + b.cfg.StartHold
	b.setSCL(false, fall)
	b.lowAt = fall
	b.end = fall
}

// RepeatedStart emits Sr (SDA falls while SCL is high) followed by SCL falling.
func (b *I2CBuilder) RepeatedStart() {
	// Release SDA high while SCL is low, then create the repeated START.
	b.setSDA(true, b.lowAt+b.cfg.DataHold)
	rise := b.lowAt + b.cfg.SCLLowTime + b.stretch
	b.stretch = 0
	b.setSCL(true, rise)
	sr := rise + b.cfg.StartSetup
	b.setSDA(false, sr)
	fall := sr + b.cfg.StartHold
	b.setSCL(false, fall)
	b.lowAt = fall
	b.end = fall
}

// Stop emits a STOP condition after the preceding ACK bit.
func (b *I2CBuilder) Stop() {
	// The last ACK bit is low; make sure SDA is low before SCL rises.
	if b.sdaHigh {
		b.setSDA(false, b.lowAt+b.cfg.DataHold)
	}
	rise := b.lowAt + b.cfg.SCLLowTime + b.stretch
	b.stretch = 0
	b.setSCL(true, rise)
	stop := rise + b.cfg.StopSetup
	b.setSDA(true, stop)
	b.end = stop
	b.lowAt = stop
}

// Bit emits one data/ACK bit sampled on the following SCL rising edge.
func (b *I2CBuilder) Bit(high bool) {
	if b.sclHigh {
		// A bit must be emitted from an SCL-low period.
		if b.err == nil {
			b.err = fmt.Errorf("testwave: I2C Bit called while SCL is high")
		}
		return
	}
	if high != b.sdaHigh {
		b.setSDA(high, b.lowAt+b.cfg.DataHold)
	}
	rise := b.lowAt + b.cfg.SCLLowTime + b.stretch
	b.stretch = 0
	b.setSCL(true, rise)
	fall := rise + b.cfg.SCLHighTime
	b.setSCL(false, fall)
	b.lowAt = fall
	b.end = fall
}

// Byte emits eight data bits (MSB first) followed by an ACK (true) or NACK
// (false) bit.
func (b *I2CBuilder) Byte(v uint8, ack bool) {
	for i := 7; i >= 0; i-- {
		b.Bit((v>>uint(i))&1 != 0)
	}
	b.Bit(!ack)
}

// Bits emits raw bit values as data bits (no extra ACK).
func (b *I2CBuilder) Bits(bits []bool) {
	for _, bit := range bits {
		b.Bit(bit)
	}
}

// EndTime returns the time of the last scheduled transition.
func (b *I2CBuilder) EndTime() float64 { return b.end }

// Transitions returns copies of the planned SCL and SDA transitions so tests
// can build channels at different sample rates.
func (b *I2CBuilder) Transitions() (scl, sda []Transition) {
	return append([]Transition(nil), b.scl...), append([]Transition(nil), b.sda...)
}

// Build synthesizes the SCL and SDA waveforms.
func (b *I2CBuilder) Build() (core.Waveform, core.Waveform, error) {
	if b.err != nil {
		return core.Waveform{}, core.Waveform{}, b.err
	}
	cfg := b.cfg.Wave
	if cfg.Duration <= 0 {
		cfg.Duration = b.end - cfg.StartTime + 2*b.cfg.SCLHighTime
	}
	scl, err := Build(cfg, true, b.scl)
	if err != nil {
		return core.Waveform{}, core.Waveform{}, err
	}
	sda, err := Build(cfg, true, b.sda)
	if err != nil {
		return core.Waveform{}, core.Waveform{}, err
	}
	return scl, sda, nil
}

func (b *I2CBuilder) setSDA(high bool, at float64) {
	if high == b.sdaHigh {
		if at > b.end {
			b.end = at
		}
		return
	}
	if at < b.end {
		at = b.end
	}
	b.sda = append(b.sda, Transition{T: at, High: high})
	b.sdaHigh = high
	if at > b.end {
		b.end = at
	}
}

func (b *I2CBuilder) setSCL(high bool, at float64) {
	if high == b.sclHigh {
		if at > b.end {
			b.end = at
		}
		return
	}
	if at < b.end {
		at = b.end
	}
	b.scl = append(b.scl, Transition{T: at, High: high})
	b.sclHigh = high
	if at > b.end {
		b.end = at
	}
}
