package tektronix

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/jkbhuan/go-decode/core"
)

// SampleEncoding describes raw curve sample encoding for the Tektronix
// adapter. Signedness and byte order must be declared explicitly.
type SampleEncoding struct {
	BytesPerPoint int
	Signed        bool
	ByteOrder     string
}

// Preamble is the subset of :WFMOutpre? used to interpret CURVe? data.
//
// Voltage = YMult * raw + YZero when YOff is zero; the manual's general form
// used here is ((raw - YOff) * YMult) + YZero. Time of sample index i is
// XZero + (i - PTOffset) * XIncrement.
type Preamble struct {
	BytesPerPoint int
	BitNumber     int
	Encoding      string
	BinaryFormat  string
	ByteOrder     string
	NumPoints     int
	PointFormat   string
	XUnit         string
	XIncrement    float64
	XZero         float64
	PTOffset      float64
	YUnit         string
	YMult         float64
	YOff          float64
	YZero         float64
	WFID          string
}

// ParseWFMOutpre parses the semicolon-separated KEY value response of
// :WFMOutpre?. Keywords are matched case-insensitively and may carry a leading
// colon; field presence varies by model/firmware, so no fixed order is assumed.
func ParseWFMOutpre(s string) (Preamble, error) {
	p := Preamble{}
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, " ")
		if !ok {
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(key, ":")))
		key = strings.TrimPrefix(key, "WFMOUTPRE:")
		val = strings.Trim(strings.TrimSpace(val), `"`)
		seen[key] = true
		f, ferr := strconv.ParseFloat(val, 64)
		switch key {
		case "BYT_NR":
			if ferr == nil {
				p.BytesPerPoint = int(f)
			}
		case "BIT_NR":
			if ferr == nil {
				p.BitNumber = int(f)
			}
		case "ENCDG":
			p.Encoding = strings.ToUpper(val)
		case "BN_FMT":
			p.BinaryFormat = strings.ToUpper(val)
		case "BYT_OR":
			p.ByteOrder = strings.ToUpper(val)
		case "NR_PT":
			if ferr == nil {
				p.NumPoints = int(f)
			}
		case "PT_FMT":
			p.PointFormat = strings.ToUpper(val)
		case "XUNIT":
			p.XUnit = val
		case "XINCR":
			p.XIncrement = f
		case "XZERO":
			p.XZero = f
		case "PT_OFF":
			p.PTOffset = f
		case "YUNIT":
			p.YUnit = val
		case "YMULT":
			p.YMult = f
		case "YOFF":
			p.YOff = f
		case "YZERO":
			p.YZero = f
		case "WFID":
			p.WFID = val
		}
	}
	if p.Encoding != "BINARY" && p.Encoding != "BIN" {
		return Preamble{}, fmt.Errorf("%w: Tektronix WFMOutpre encoding %q (ASCII is not a binary transfer)", core.ErrUnsupportedAcquisitionType, p.Encoding)
	}
	if p.BytesPerPoint <= 0 {
		return Preamble{}, fmt.Errorf("decode: Tektronix WFMOutpre missing BYT_NR")
	}
	if p.PointFormat != "" && !strings.HasPrefix(p.PointFormat, "Y") {
		return Preamble{}, fmt.Errorf("%w: Tektronix PT_FMT %q (only Y/YT is supported)", core.ErrUnsupportedAcquisitionType, p.PointFormat)
	}
	if !core.IsFinite(p.XIncrement) || p.XIncrement <= 0 ||
		!core.IsFinite(p.XZero) || !core.IsFinite(p.PTOffset) ||
		!core.IsFinite(p.YMult) || !core.IsFinite(p.YOff) || !core.IsFinite(p.YZero) {
		return Preamble{}, fmt.Errorf("decode: invalid Tektronix WFMOutpre scaling")
	}
	if p.BinaryFormat == "FP" || p.BinaryFormat == "FPB" {
		return Preamble{}, fmt.Errorf("%w: Tektronix floating-point binary format is not supported by this reader", core.ErrUnsupportedAcquisitionType)
	}
	return p, nil
}

// Encoding returns the explicit sample encoding described by the preamble.
func (p Preamble) EncodingSpec() (SampleEncoding, error) {
	order := strings.ToUpper(strings.TrimSpace(p.ByteOrder))
	switch order {
	case "LSB", "LSBFIRST", "LITTLE":
		order = "little"
	case "MSB", "MSBFIRST", "BIG":
		order = "big"
	default:
		return SampleEncoding{}, fmt.Errorf("decode: unknown Tektronix WFMOutpre BYT_OR %q", p.ByteOrder)
	}
	signed := true
	switch p.BinaryFormat {
	case "RI":
		signed = true
	case "RP":
		signed = false
	default:
		return SampleEncoding{}, fmt.Errorf("decode: unsupported Tektronix BN_FMT %q", p.BinaryFormat)
	}
	return SampleEncoding{BytesPerPoint: p.BytesPerPoint, Signed: signed, ByteOrder: order}, nil
}

// FormatWaveform converts a CURVe? payload using WFMOutpre scaling and the
// caller-declared sample encoding. It never uses the Keysight scaling formula.
func FormatWaveform(payload []byte, p Preamble, encoding SampleEncoding) (core.Waveform, error) {
	if encoding.BytesPerPoint <= 0 {
		return core.Waveform{}, fmt.Errorf("decode: invalid Tektronix bytes-per-point")
	}
	if len(payload)%encoding.BytesPerPoint != 0 {
		return core.Waveform{}, fmt.Errorf("decode: Tektronix payload length %d is not a multiple of %d", len(payload), encoding.BytesPerPoint)
	}
	if p.NumPoints > 0 && len(payload)/encoding.BytesPerPoint != p.NumPoints {
		return core.Waveform{}, fmt.Errorf("decode: Tektronix payload has %d points, WFMOutpre says %d", len(payload)/encoding.BytesPerPoint, p.NumPoints)
	}
	if !core.IsFinite(p.XIncrement) || p.XIncrement <= 0 ||
		!core.IsFinite(p.XZero) || !core.IsFinite(p.PTOffset) ||
		!core.IsFinite(p.YMult) || !core.IsFinite(p.YOff) || !core.IsFinite(p.YZero) {
		return core.Waveform{}, fmt.Errorf("decode: invalid Tektronix preamble scaling")
	}
	var order binary.ByteOrder
	switch strings.ToLower(strings.TrimSpace(encoding.ByteOrder)) {
	case "little", "lsb", "lsbfirst", "lsb_first":
		order = binary.LittleEndian
	case "big", "msb", "msbfirst", "msb_first":
		order = binary.BigEndian
	default:
		return core.Waveform{}, fmt.Errorf("decode: unknown Tektronix byte order %q", encoding.ByteOrder)
	}
	n := len(payload) / encoding.BytesPerPoint
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		raw, err := decodeRaw(payload[i*encoding.BytesPerPoint:(i+1)*encoding.BytesPerPoint], encoding.BytesPerPoint, encoding.Signed, order)
		if err != nil {
			return core.Waveform{}, err
		}
		v := float32((raw-p.YOff)*p.YMult + p.YZero)
		if !core.IsFinite(float64(v)) {
			return core.Waveform{}, fmt.Errorf("decode: Tektronix sample %d is not finite", i)
		}
		samples[i] = v
	}
	w := core.Waveform{
		Samples:    samples,
		SampleRate: 1 / p.XIncrement,
		StartTime:  p.XZero - p.PTOffset*p.XIncrement,
	}
	if err := w.Validate(); err != nil {
		return core.Waveform{}, err
	}
	return w, nil
}

func decodeRaw(b []byte, bpp int, signed bool, order binary.ByteOrder) (float64, error) {
	switch bpp {
	case 1:
		if signed {
			return float64(int8(b[0])), nil
		}
		return float64(b[0]), nil
	case 2:
		u := order.Uint16(b)
		if signed {
			return float64(int16(u)), nil
		}
		return float64(u), nil
	case 4:
		u := order.Uint32(b)
		if signed {
			return float64(int32(u)), nil
		}
		return float64(u), nil
	case 8:
		u := order.Uint64(b)
		if signed {
			return float64(int64(u)), nil
		}
		return float64(u), nil
	default:
		return 0, fmt.Errorf("decode: unsupported Tektronix sample width %d", bpp)
	}
}
