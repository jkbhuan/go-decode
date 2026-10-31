package demo

import "github.com/jkbhuan/go-decode"

// I2CExampleTitle describes the synthesized I2C example.
const I2CExampleTitle = "I2C DEMO: START + ADDR 0X50 WRITE + DATA 0X10 0X20 + STOP"

// I2CExample is the synthesized I2C demo waveform pair.
type I2CExample struct {
	SCL decode.Waveform
	SDA decode.Waveform
}

type busConfig struct {
	sclLow    float64
	sclHigh   float64
	dataHold  float64
	startHold float64
	stopSetup float64
}

// BuildI2CExample creates the waveform used by the I2C example command.
func BuildI2CExample() (I2CExample, error) {
	const startTime = -2e-6
	s := &i2cSched{
		t:       startTime + 1e-6, // idle high period before START
		sclHigh: true,
		sdaHigh: true,
		cfg:     busConfig{sclLow: 5e-6, sclHigh: 2e-6, dataHold: 1e-6, startHold: 0.5e-6, stopSetup: 0.8e-6},
	}
	s.start()
	s.dataByte(0xA0) // 7-bit address 0x50, write
	s.dataByte(0x10)
	s.dataByte(0x20)
	s.stop()

	cfg := Config{
		StartTime:  startTime,
		SampleRate: 100e6,
		Duration:   s.t - startTime + 5e-6,
		Low:        0,
		High:       3.3,
	}
	scl, err := Build(cfg, true, s.scl)
	if err != nil {
		return I2CExample{}, err
	}
	sda, err := Build(cfg, true, s.sda)
	if err != nil {
		return I2CExample{}, err
	}
	return I2CExample{SCL: scl, SDA: sda}, nil
}

type i2cSched struct {
	cfg     busConfig
	t       float64
	sclHigh bool
	sdaHigh bool
	scl     []Transition
	sda     []Transition
}

func (s *i2cSched) setSCL(high bool, at float64) {
	if high == s.sclHigh {
		return
	}
	s.scl = append(s.scl, Transition{T: at, High: high})
	s.sclHigh = high
}

func (s *i2cSched) setSDA(high bool, at float64) {
	if high == s.sdaHigh {
		return
	}
	s.sda = append(s.sda, Transition{T: at, High: high})
	s.sdaHigh = high
}

func (s *i2cSched) start() {
	s.setSDA(false, s.t)
	s.setSCL(false, s.t+s.cfg.startHold)
	s.t += s.cfg.startHold
}

func (s *i2cSched) stop() {
	if s.sdaHigh {
		s.setSDA(false, s.t+s.cfg.dataHold)
	}
	rise := s.t + s.cfg.sclLow
	s.setSCL(true, rise)
	s.setSDA(true, rise+s.cfg.stopSetup)
	s.t = rise + s.cfg.stopSetup
}

func (s *i2cSched) bit(high bool) {
	if high != s.sdaHigh {
		s.setSDA(high, s.t+s.cfg.dataHold)
	}
	rise := s.t + s.cfg.sclLow
	s.setSCL(true, rise)
	fall := rise + s.cfg.sclHigh
	s.setSCL(false, fall)
	s.t = fall
}

func (s *i2cSched) dataByte(v uint8) {
	for i := 7; i >= 0; i-- {
		s.bit((v>>uint(i))&1 != 0)
	}
	s.bit(false) // ACK from the target
}
