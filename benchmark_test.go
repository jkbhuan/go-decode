package decode_test

import (
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

func BenchmarkExtractLogicTrace(b *testing.B) {
	const n = 200000
	samples := make([]float64, n)
	for i := range samples {
		if (i/500)%2 == 0 {
			samples[i] = 3.3
		}
	}
	w := decode.Waveform{Samples: samples, SampleRate: 1e9, StartTime: -1e-3}
	threshold := decode.LogicThreshold{Low: 0.99, High: 2.31}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decode.ExtractLogicTrace(w, threshold, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeI2C(b *testing.B) {
	builder := mustI2CBuilder(b, testwave.I2CConfig{
		Wave:        defaultWaveConfig(0, 100e6, 0),
		SCLHighTime: 2e-6,
		SCLLowTime:  5e-6,
		DataHold:    1e-6,
		DataSetup:   4e-6,
		StartHold:   0.5e-6,
		StartSetup:  1.5e-6,
		StopSetup:   0.8e-6,
	})
	builder.Idle(1e-6)
	for i := 0; i < 20; i++ {
		builder.Start()
		builder.Byte(0xA0, true)
		builder.Byte(uint8(i), true)
		builder.Byte(uint8(0xFF-i), true)
		builder.Stop()
		builder.Idle(5e-6)
	}
	scl, sda, err := builder.Build()
	if err != nil {
		b.Fatal(err)
	}
	opts := baseI2COptions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := decode.DecodeI2C(scl, sda, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Transactions) != 20 {
			b.Fatalf("transactions = %d", len(res.Transactions))
		}
	}
}

func BenchmarkDecodeSMI(b *testing.B) {
	builder := mustMDIOBuilder(b, testwave.MDIOConfig{
		Wave:        defaultWaveConfig(0, 100e6, 0),
		MDCPeriod:   1e-6,
		MDCHighTime: 0.5e-6,
		DataHold:    0.2e-6,
		DataSetup:   0.3e-6,
	})
	for i := 0; i < 20; i++ {
		builder.Preamble(32)
		builder.Frame(decode.DirectionWrite, uint8(i), 0x10, uint16(i*17))
	}
	mdc, mdio, err := builder.Build()
	if err != nil {
		b.Fatal(err)
	}
	opts := baseSMIOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := decode.DecodeSMI(mdc, mdio, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Frames) != 20 {
			b.Fatalf("res.Frames = %d", len(res.Frames))
		}
	}
}

func BenchmarkFormatKeysightWaveform(b *testing.B) {
	const points = 100000
	payload := make([]byte, points*2)
	for i := 0; i < points; i++ {
		payload[2*i] = byte(i)
		payload[2*i+1] = byte(i >> 8)
	}
	p := decode.KeysightPreamble{
		XIncrement: 1e-9, XOrigin: 0, XReference: 0,
		YIncrement: 1e-3, YOrigin: 0, YReference: 0,
	}
	enc := decode.SampleEncoding{BytesPerPoint: 2, Signed: true, ByteOrder: "little"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decode.FormatKeysightWaveform(payload, p, enc); err != nil {
			b.Fatal(err)
		}
	}
}
