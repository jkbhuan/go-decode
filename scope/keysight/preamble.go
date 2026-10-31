package keysight

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jkbhuan/go-decode/core"
)

// Preamble is the scaling block returned by :WAVeform:PREamble? for
// Keysight InfiniiVision-family instruments.
//
// Voltage(i) = (Raw(i) - YReference) * YIncrement + YOrigin
// SampleRate = 1 / XIncrement
// StartTime  = XOrigin - XReference * XIncrement
// Time(i)    = StartTime + i * XIncrement
//
// The formulas are documented in the InfiniiVision 3000 X-Series Programmer's
// Guide for :WAVeform:PREamble (and in the Infiniium MXR guide for the
// equivalent preamble). They must be checked against the specific model and
// the selected :WAVeform:FORMat / :WAVeform:UNSigned setting: some families
// use XReference = 0, and BYTE/WORD raw values differ in signedness. This
// package therefore requires the encoding to be supplied explicitly rather
// than guessing it.
type Preamble struct {
	XIncrement float64
	XOrigin    float64
	XReference float64
	YIncrement float64
	YOrigin    float64
	YReference float64
}

// PreambleInfo contains the non-scaling fields of the preamble block.
// The meaning of Format is family-dependent (see ParseKeysightPreamble); Type
// is likewise documented per model. They are reported verbatim.
type PreambleInfo struct {
	Format int
	Type   int
	Points int
	Count  int
}

// SampleEncoding describes how raw payload bytes encode sample values.
//
// ByteOrder only affects multi-byte samples, but a non-empty value is always
// validated. Accepted spellings are "little"/"lsbfirst"/"lsb" and
// "big"/"msbfirst"/"msb" (case-insensitive).
type SampleEncoding struct {
	BytesPerPoint int
	Signed        bool
	ByteOrder     string
}

var (
	ErrInvalidPreamble = errors.New("decode: invalid Keysight preamble")
	ErrInvalidEncoding = errors.New("decode: invalid sample encoding")
	ErrPayloadLength   = errors.New("decode: payload length does not match bytes-per-point")
)

// Validate checks the preamble scaling fields.
func (p Preamble) Validate() error {
	fields := []struct {
		name string
		v    float64
	}{
		{"x increment", p.XIncrement},
		{"x origin", p.XOrigin},
		{"x reference", p.XReference},
		{"y increment", p.YIncrement},
		{"y origin", p.YOrigin},
		{"y reference", p.YReference},
	}
	for _, f := range fields {
		if !core.IsFinite(f.v) {
			return fmt.Errorf("%w: %s is not finite", ErrInvalidPreamble, f.name)
		}
	}
	if p.XIncrement <= 0 {
		return fmt.Errorf("%w: x increment must be > 0, got %g", ErrInvalidPreamble, p.XIncrement)
	}
	if p.YIncrement <= 0 {
		return fmt.Errorf("%w: y increment must be > 0, got %g", ErrInvalidPreamble, p.YIncrement)
	}
	return nil
}

// Validate checks the encoding fields.
func (e SampleEncoding) Validate() error {
	switch e.BytesPerPoint {
	case 1, 2, 4:
	default:
		return fmt.Errorf("%w: bytes per point must be 1, 2 or 4, got %d", ErrInvalidEncoding, e.BytesPerPoint)
	}
	// Byte order is irrelevant for one byte, but a supplied value must still
	// be valid so a caller's typo is not silently ignored.
	if e.BytesPerPoint > 1 || strings.TrimSpace(e.ByteOrder) != "" {
		if _, err := normalizedByteOrder(e.ByteOrder); err != nil {
			return err
		}
	}
	return nil
}

func normalizedByteOrder(s string) (binary.ByteOrder, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "little", "lsb", "lsbf", "lsbfirst", "lsb_first":
		return binary.LittleEndian, nil
	case "big", "msb", "msbf", "msbfirst", "msb_first":
		return binary.BigEndian, nil
	default:
		return nil, fmt.Errorf("%w: unknown byte order %q (use little/LSBFirst or big/MSBFirst)", ErrInvalidEncoding, s)
	}
}

// ParseKeysightPreamble parses the comma-separated text response of
// :WAVeform:PREamble?. The first ten fields, shared by InfiniiVision and
// Infiniium (MXR/EXR) families, are
// format,type,points,count,xincrement,xorigin,xreference,yincrement,yorigin,yreference.
// Infiniium returns additional fields after these (coupling, display ranges,
// date/time, ...); they are ignored. At least ten fields are required.
//
// The meaning of the <format> code is family-dependent (InfiniiVision: 0=BYTE,
// 1=WORD, 4=ASCii; Infiniium: 0=ASCii, 1=BYTE, 2=WORD, ...), so it is reported
// verbatim and callers must supply SampleEncoding explicitly.
func ParsePreamble(s string) (Preamble, PreambleInfo, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) < 10 {
		return Preamble{}, PreambleInfo{}, fmt.Errorf(
			"%w: expected at least 10 comma-separated fields, got %d", ErrInvalidPreamble, len(parts))
	}
	vals := make([]float64, 10)
	for i, raw := range parts[:10] {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || !core.IsFinite(v) {
			return Preamble{}, PreambleInfo{}, fmt.Errorf(
				"%w: field %d (%q) is not a finite number", ErrInvalidPreamble, i, raw)
		}
		vals[i] = v
	}
	info := PreambleInfo{
		Format: int(vals[0]),
		Type:   int(vals[1]),
		Points: int(vals[2]),
		Count:  int(vals[3]),
	}
	p := Preamble{
		XIncrement: vals[4],
		XOrigin:    vals[5],
		XReference: vals[6],
		YIncrement: vals[7],
		YOrigin:    vals[8],
		YReference: vals[9],
	}
	if err := p.Validate(); err != nil {
		return Preamble{}, PreambleInfo{}, err
	}
	return p, info, nil
}

// FormatKeysightWaveform converts a stripped SCPI binary payload (no IEEE
// 488.2 block header) into a core.Waveform using the supplied preamble and
// explicitly declared sample encoding.
func FormatWaveform(payload []byte, p Preamble, encoding SampleEncoding) (core.Waveform, error) {
	if err := p.Validate(); err != nil {
		return core.Waveform{}, err
	}
	if err := encoding.Validate(); err != nil {
		return core.Waveform{}, err
	}
	bpp := encoding.BytesPerPoint
	if len(payload) == 0 {
		return core.Waveform{}, fmt.Errorf("%w: payload is empty", ErrPayloadLength)
	}
	if len(payload)%bpp != 0 {
		return core.Waveform{}, fmt.Errorf("%w: %d bytes is not a multiple of %d", ErrPayloadLength, len(payload), bpp)
	}

	var order binary.ByteOrder
	if bpp > 1 {
		var err error
		order, err = normalizedByteOrder(encoding.ByteOrder)
		if err != nil {
			return core.Waveform{}, err
		}
	}

	n := len(payload) / bpp
	samples := make([]float64, n)
	for i := 0; i < n; i++ {
		raw := decodeRaw(payload[i*bpp:(i+1)*bpp], bpp, encoding.Signed, order)
		v := (raw-p.YReference)*p.YIncrement + p.YOrigin
		if !core.IsFinite(v) {
			return core.Waveform{}, fmt.Errorf("%w: converted sample %d is not finite", ErrInvalidPreamble, i)
		}
		samples[i] = v
	}

	w := core.Waveform{
		Samples:    samples,
		SampleRate: 1 / p.XIncrement,
		StartTime:  p.XOrigin - p.XReference*p.XIncrement,
	}
	if err := w.Validate(); err != nil {
		return core.Waveform{}, err
	}
	return w, nil
}

func decodeRaw(b []byte, bpp int, signed bool, order binary.ByteOrder) float64 {
	switch bpp {
	case 1:
		if signed {
			return float64(int8(b[0]))
		}
		return float64(b[0])
	case 2:
		u := order.Uint16(b)
		if signed {
			return float64(int16(u))
		}
		return float64(u)
	case 4:
		u := order.Uint32(b)
		if signed {
			return float64(int32(u))
		}
		return float64(u)
	default:
		return math.NaN()
	}
}
