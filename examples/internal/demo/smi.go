package demo

import "github.com/jkbhuan/go-decode"

// SMIExampleTitle describes the synthesized MDIO Clause 22 example.
const SMIExampleTitle = "MDIO CLAUSE 22: PRE 32 + WRITE PHY 0X05 REG 0X1F DATA 0XBEEF"

// SMIExample is the synthesized MDIO demo waveform pair.
type SMIExample struct {
	MDC  decode.Waveform
	MDIO decode.Waveform
}

// BuildSMIExample creates the waveform used by the SMI example command.
func BuildSMIExample() (SMIExample, error) {
	const (
		startTime = -1e-6
		period    = 1e-6
		highTime  = 0.5e-6
		lowTime   = period - highTime
		holdTime  = 0.2e-6
	)

	// PRE(32) + ST(01) + OP(01 write) + PHYAD(0x05) + REGAD(0x1F) +
	// TA(10) + DATA(0xBEEF).
	var bits []bool
	for i := 0; i < 32; i++ {
		bits = append(bits, true)
	}
	bits = appendMSB(bits, 0x01, 2)
	bits = appendMSB(bits, 0x01, 2)
	bits = appendMSB(bits, 0x05, 5)
	bits = appendMSB(bits, 0x1F, 5)
	bits = appendMSB(bits, 0x02, 2)
	bits = appendMSB(bits, 0xBEEF, 16)

	cycle := startTime
	mdioHigh := true
	mdc := []Transition{{T: cycle, High: false}}
	var mdio []Transition
	for _, bit := range bits {
		if bit != mdioHigh {
			mdio = append(mdio, Transition{T: cycle + holdTime, High: bit})
			mdioHigh = bit
		}
		mdc = append(mdc, Transition{T: cycle + lowTime, High: true})
		mdc = append(mdc, Transition{T: cycle + period, High: false})
		cycle += period
	}

	cfg := Config{
		StartTime:  startTime,
		SampleRate: 100e6,
		Duration:   cycle - startTime,
		Low:        0,
		High:       3.3,
	}
	mdcWave, err := Build(cfg, true, mdc)
	if err != nil {
		return SMIExample{}, err
	}
	mdioWave, err := Build(cfg, true, mdio)
	if err != nil {
		return SMIExample{}, err
	}
	return SMIExample{MDC: mdcWave, MDIO: mdioWave}, nil
}

func appendMSB(dst []bool, v uint32, n int) []bool {
	for i := n - 1; i >= 0; i-- {
		dst = append(dst, (v>>uint(i))&1 != 0)
	}
	return dst
}
