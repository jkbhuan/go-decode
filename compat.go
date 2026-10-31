package decode

import (
	"github.com/jkbhuan/go-decode/scope"
	"github.com/jkbhuan/go-decode/scope/keysight"
	"github.com/jkbhuan/go-decode/scope/tektronix"
)

// Keysight SCPI payload compatibility API.
type (
	KeysightPreamble     = keysight.Preamble
	KeysightPreambleInfo = keysight.PreambleInfo
	SampleEncoding       = keysight.SampleEncoding
)

var (
	ErrInvalidPreamble = keysight.ErrInvalidPreamble
	ErrInvalidEncoding = keysight.ErrInvalidEncoding
	ErrPayloadLength   = keysight.ErrPayloadLength
	ErrInvalidBlock    = scope.ErrInvalidBlock
)

// ParseKeysightPreamble parses :WAVeform:PREamble? text.
func ParseKeysightPreamble(s string) (KeysightPreamble, KeysightPreambleInfo, error) {
	return keysight.ParsePreamble(s)
}

// FormatKeysightWaveform converts a stripped SCPI payload to volts/seconds.
func FormatKeysightWaveform(payload []byte, p KeysightPreamble, enc SampleEncoding) (Waveform, error) {
	return keysight.FormatWaveform(payload, p, enc)
}

// ParseIEEE4882DefiniteBlock parses a definite-length IEEE 488.2 block.
func ParseIEEE4882DefiniteBlock(data []byte) (payload, rest []byte, err error) {
	return scope.ParseIEEE4882DefiniteBlock(data)
}

// Tektronix compatibility API.
type TektronixPreamble = tektronix.Preamble

// FormatTektronixWaveform is reserved for the Tektronix implementation.
func FormatTektronixWaveform(payload []byte, p TektronixPreamble, enc SampleEncoding) (Waveform, error) {
	return tektronix.FormatWaveform(payload, p, tektronix.SampleEncoding{
		BytesPerPoint: enc.BytesPerPoint,
		Signed:        enc.Signed,
		ByteOrder:     enc.ByteOrder,
	})
}
