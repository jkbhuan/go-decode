package decode_test

import (
	"testing"

	"github.com/jkbhuan/go-decode"
)

func FuzzFormatKeysightWaveform(f *testing.F) {
	f.Add([]byte{0, 128, 255}, uint8(1), false, uint8(0))
	f.Add([]byte{1, 2, 3, 4}, uint8(2), true, uint8(1))
	f.Add([]byte{}, uint8(1), true, uint8(0))
	f.Add([]byte{1, 2, 3}, uint8(4), false, uint8(2))
	f.Fuzz(func(t *testing.T, payload []byte, bpp uint8, signed bool, orderSel uint8) {
		order := ""
		switch orderSel % 3 {
		case 0:
			order = "little"
		case 1:
			order = "big"
		default:
			order = "unknown"
		}
		enc := decode.SampleEncoding{BytesPerPoint: int(bpp), Signed: signed, ByteOrder: order}
		p := decode.KeysightPreamble{
			XIncrement: 1e-9, XOrigin: -1e-6, XReference: 0,
			YIncrement: 1e-3, YOrigin: 0, YReference: 128,
		}
		w, err := decode.FormatKeysightWaveform(payload, p, enc)
		if err != nil {
			return
		}
		if err := w.Validate(); err != nil {
			t.Fatalf("successful conversion produced an invalid waveform: %v", err)
		}
	})
}

func FuzzParseIEEE4882DefiniteBlock(f *testing.F) {
	f.Add([]byte("#42000"))
	f.Add([]byte("#0"))
	f.Add([]byte("#15abcX"))
	f.Add([]byte("garbage"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = decode.ParseIEEE4882DefiniteBlock(data)
	})
}

func FuzzDecodeI2C(f *testing.F) {
	f.Add([]byte{0xAA, 0x55, 0xCC, 0x33}, uint8(3))
	f.Add([]byte{0x00}, uint8(0))
	f.Add([]byte{0xFF, 0xFF}, uint8(7))
	f.Fuzz(func(t *testing.T, packed []byte, glitchSel uint8) {
		if len(packed) < 4 {
			return
		}
		sclSamples := make([]float64, len(packed))
		sdaSamples := make([]float64, len(packed))
		for i, b := range packed {
			sclSamples[i] = float64(b&1) * 3.3
			sdaSamples[i] = float64((b>>1)&1) * 3.3
		}
		scl := decode.Waveform{Samples: sclSamples, SampleRate: 1e6, StartTime: -1e-6}
		sda := decode.Waveform{Samples: sdaSamples, SampleRate: 1e6, StartTime: -1e-6}
		glitch := 0.0
		if glitchSel%2 == 0 {
			glitch = 5e-7
		}
		opts := decode.I2COptions{
			SCLThreshold: decode.LogicThreshold{Low: 1.0, High: 2.0},
			SDAThreshold: decode.LogicThreshold{Low: 1.0, High: 2.0},
			VDD:          3.3,
			GlitchWidth:  glitch,
		}
		_, err := decode.DecodeI2C(scl, sda, opts)
		if err != nil {
			return
		}
	})
}

func FuzzDecodeSMI(f *testing.F) {
	f.Add([]byte{0xAA, 0x55, 0xCC, 0x33, 0xF0, 0x0F}, uint8(1))
	f.Add([]byte{0xFF}, uint8(0))
	f.Fuzz(func(t *testing.T, packed []byte, suppressionSel uint8) {
		if len(packed) < 4 {
			return
		}
		mdcSamples := make([]float64, len(packed))
		mdioSamples := make([]float64, len(packed))
		for i, b := range packed {
			mdcSamples[i] = float64(b&1) * 3.3
			mdioSamples[i] = float64((b>>1)&1) * 3.3
		}
		mdc := decode.Waveform{Samples: mdcSamples, SampleRate: 1e6, StartTime: 0}
		mdio := decode.Waveform{Samples: mdioSamples, SampleRate: 1e6, StartTime: 0}
		opts := decode.SMIOptions{
			MDCThreshold:             decode.LogicThreshold{Low: 1.0, High: 2.0},
			MDIOThreshold:            decode.LogicThreshold{Low: 1.0, High: 2.0},
			AllowPreambleSuppression: suppressionSel%2 == 0,
		}
		_, err := decode.DecodeSMI(mdc, mdio, opts)
		if err != nil {
			return
		}
	})
}
