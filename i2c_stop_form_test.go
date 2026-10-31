package decode_test

import (
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

// A STOP can also be generated during the ACK bit's SCL high period (SDA
// returns high while SCL stays high), without an extra SCL edge. This test
// builds that form manually so the decoder is not tied to one generator's
// stop convention.
func TestI2CStopDuringAckHighPeriod(t *testing.T) {
	cfg := defaultWaveConfig(0, 200e6, 60e-6)
	var scl, sda []testwave.Transition
	sclHigh, sdaHigh := true, true
	setSCL := func(high bool, at float64) {
		if high != sclHigh {
			scl = append(scl, testwave.Transition{T: at, High: high})
			sclHigh = high
		}
	}
	setSDA := func(high bool, at float64) {
		if high != sdaHigh {
			sda = append(sda, testwave.Transition{T: at, High: high})
			sdaHigh = high
		}
	}

	// Idle, then START.
	setSDA(false, 1e-6)
	setSCL(false, 1.5e-6)
	lowAt := 1.5e-6

	// Address 0x50 + write = 1010_0000.
	addr := uint8(0xA0)
	for i := 7; i >= 0; i-- {
		bitHigh := (addr>>uint(i))&1 != 0
		setSDA(bitHigh, lowAt+1e-6)
		rise := lowAt + 4e-6
		setSCL(true, rise)
		fall := rise + 2e-6
		setSCL(false, fall)
		lowAt = fall
	}
	// ACK: SDA low during the ninth clock's low period.
	setSDA(false, lowAt+1e-6)
	ackRise := lowAt + 4e-6
	setSCL(true, ackRise)
	// STOP while SCL is still high: SDA rises 0.3 us after SCL rise.
	setSDA(true, ackRise+0.3e-6)
	// SCL eventually falls; it is outside the STOP condition.
	setSCL(false, ackRise+2e-6)

	sclWave, err := testwave.Build(cfg, true, scl)
	if err != nil {
		t.Fatal(err)
	}
	sdaWave, err := testwave.Build(cfg, true, sda)
	if err != nil {
		t.Fatal(err)
	}

	res, err := decode.DecodeI2C(sclWave, sdaWave, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d: %+v", len(res.Transactions), res.Diagnostics)
	}
	tx := res.Transactions[0]
	if len(tx.Frames) != 1 {
		t.Fatalf("frames = %d", len(tx.Frames))
	}
	f := tx.Frames[0]
	if f.Address != 0x50 || f.Direction != decode.DirectionWrite || f.AddressACK != decode.ACK {
		t.Fatalf("frame = %+v", f)
	}
	if !f.HasStop || !f.Complete || !tx.Complete {
		t.Fatalf("STOP in ACK high period not recognized: %+v (tx diags %+v)", f, tx.Diagnostics)
	}
	if len(f.Data) != 0 {
		t.Fatalf("no data bytes expected, got %+v", f.Data)
	}
	stos := findTiming(tx.Timings, decode.TimingSUSTO)
	if len(stos) != 1 {
		t.Fatalf("tSU;STO candidates = %d: %+v", len(stos), tx.Diagnostics)
	}
	approx(t, stos[0].Value, 0.3e-6, 2.0/cfg.SampleRate, "tSU;STO")
	if hasDiag(tx.Diagnostics, decode.CodeI2CStopMidByte) || hasDiag(tx.Diagnostics, decode.CodeI2CTruncatedByte) {
		t.Fatalf("unexpected truncation diagnostic: %+v", tx.Diagnostics)
	}
}
