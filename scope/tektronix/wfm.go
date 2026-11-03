package tektronix

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/jkbhuan/go-decode/core"
)

// WFMOptions limits saved .wfm reads and supplies caller-known metadata that
// is not present in the file (for example the channel label).
type WFMOptions struct {
	MaxBytes  int64
	MaxPoints int64
	Channel   string
}

func (o WFMOptions) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return 512 << 20
}

func (o WFMOptions) maxPoints() int64 {
	if o.MaxPoints > 0 {
		return o.MaxPoints
	}
	return 100_000_000
}

// ReadWFM parses a Tektronix WFM#002/#003 saved reference-waveform file.
//
// Supported: little/big endian, normal YT vector analog waveforms, int8/int16/
// int32/uint32/uint64/float32/float64 curve formats, single (non-FastFrame)
// records. FastFrame, envelope/peak, and non-vector records return
// core.ErrUnsupportedAcquisitionType instead of being misrepresented as normal
// equal-interval waveforms.
func ReadWFM(r io.ReaderAt, size int64, opts WFMOptions) (core.Capture, error) {
	if size < 78 || size > opts.maxBytes() {
		return core.Capture{}, fmt.Errorf("%w: size %d", core.ErrFileTooLarge, size)
	}
	head := make([]byte, 156)
	if _, err := r.ReadAt(head, 0); err != nil {
		return core.Capture{}, err
	}
	static := head[:78]
	var order binary.ByteOrder
	switch binary.LittleEndian.Uint16(static[0:2]) {
	case 0x0F0F:
		order = binary.LittleEndian
	case 0xF0F0:
		order = binary.BigEndian
	default:
		return core.Capture{}, fmt.Errorf("%w: unknown Tektronix byte-order marker %#x", core.ErrUnknownFormat, binary.LittleEndian.Uint16(static[0:2]))
	}
	if static[2] != ':' {
		return core.Capture{}, fmt.Errorf("%w: missing ':' after byte-order marker", core.ErrUnknownFormat)
	}
	version := string(static[3:10])
	if version != "WFM#002" && version != "WFM#003" {
		return core.Capture{}, fmt.Errorf("%w: %q", core.ErrUnsupportedFormatVersion, version)
	}
	bpp := int64(static[15])
	curveOffset := int64(int32(order.Uint32(static[16:20])))
	nframesMinus1 := order.Uint32(static[72:76])
	label := cString(static[40:72])
	if bpp < 1 || bpp > 8 {
		return core.Capture{}, fmt.Errorf("%w: bytes-per-point %d", core.ErrUnsupportedFormatVersion, bpp)
	}
	if curveOffset < 78 || curveOffset > size {
		return core.Capture{}, fmt.Errorf("decode: Tektronix curve offset %d outside file", curveOffset)
	}
	if nframesMinus1 > 0 {
		return core.Capture{}, fmt.Errorf("%w: Tektronix FastFrame (%d frames) is not supported", core.ErrUnsupportedAcquisitionType, nframesMinus1+1)
	}

	setType := int32(order.Uint32(head[78:82]))
	dataType := int32(order.Uint32(head[122:126]))
	summary := order.Uint16(head[154:156])
	if setType != 0 {
		return core.Capture{}, fmt.Errorf("%w: Tektronix set type %d", core.ErrUnsupportedAcquisitionType, setType)
	}
	if dataType != 2 {
		return core.Capture{}, fmt.Errorf("%w: Tektronix data type %d (only vector YT is supported)", core.ErrUnsupportedAcquisitionType, dataType)
	}
	if summary != 0 {
		return core.Capture{}, fmt.Errorf("%w: Tektronix summary/envelope frame type %d", core.ErrUnsupportedAcquisitionType, summary)
	}

	var expOff, impOff, curveObjOff int64
	var expSize, impSize int64
	if version == "WFM#003" {
		expOff, expSize = 168, 160
		impOff, impSize = 488, 136
		curveObjOff = 808
	} else {
		expOff, expSize = 168, 156
		impOff, impSize = 480, 132
		curveObjOff = 792
	}
	if curveObjOff+30 > curveOffset {
		return core.Capture{}, fmt.Errorf("decode: Tektronix curve object overlaps curve buffer")
	}
	exp := make([]byte, expSize)
	imp := make([]byte, impSize)
	curveObj := make([]byte, 30)
	if _, err := r.ReadAt(exp, expOff); err != nil {
		return core.Capture{}, err
	}
	if _, err := r.ReadAt(imp, impOff); err != nil {
		return core.Capture{}, err
	}
	if _, err := r.ReadAt(curveObj, curveObjOff); err != nil {
		return core.Capture{}, err
	}

	expScale := math.Float64frombits(order.Uint64(exp[0:8]))
	expOffset := math.Float64frombits(order.Uint64(exp[8:16]))
	expUnits := cString(exp[20:40])
	expFormat := int32(order.Uint32(exp[72:76]))
	expStorage := int32(order.Uint32(exp[76:80]))

	impScale := math.Float64frombits(order.Uint64(imp[0:8]))
	impOffset := math.Float64frombits(order.Uint64(imp[8:16]))
	impSizePoints := int64(order.Uint32(imp[16:20]))
	impUnits := cString(imp[20:40])

	if !core.IsFinite(expScale) || !core.IsFinite(expOffset) || !core.IsFinite(impScale) || !core.IsFinite(impOffset) {
		return core.Capture{}, fmt.Errorf("decode: non-finite Tektronix scaling")
	}
	if impScale <= 0 {
		return core.Capture{}, fmt.Errorf("decode: invalid Tektronix X increment %g", impScale)
	}
	if expStorage != 0 {
		return core.Capture{}, fmt.Errorf("%w: Tektronix storage type %d", core.ErrUnsupportedAcquisitionType, expStorage)
	}
	formatBPP, signed, isFloat, ok := formatInfo(expFormat)
	if !ok {
		return core.Capture{}, fmt.Errorf("%w: Tektronix explicit format %d", core.ErrUnsupportedFormatVersion, expFormat)
	}
	if formatBPP != bpp {
		return core.Capture{}, fmt.Errorf("decode: Tektronix format %d implies %d bytes/point, file says %d", expFormat, formatBPP, bpp)
	}
	if expUnits != "" && !strings.EqualFold(expUnits, "V") && !strings.EqualFold(expUnits, "Volts") {
		return core.Capture{}, fmt.Errorf("%w: Tektronix Y units %q", core.ErrUnsupportedAcquisitionType, expUnits)
	}
	if impUnits != "" && !strings.EqualFold(impUnits, "s") && !strings.EqualFold(impUnits, "sec") {
		return core.Capture{}, fmt.Errorf("%w: Tektronix X units %q", core.ErrUnsupportedAcquisitionType, impUnits)
	}

	prechargeStart := int64(order.Uint32(curveObj[10:14]))
	dataStart := int64(order.Uint32(curveObj[14:18]))
	postchargeStart := int64(order.Uint32(curveObj[18:22]))
	postchargeStop := int64(order.Uint32(curveObj[22:26]))
	endOfBuffer := int64(order.Uint32(curveObj[26:30]))
	if dataStart > postchargeStart || postchargeStart > postchargeStop || postchargeStop > endOfBuffer {
		return core.Capture{}, fmt.Errorf("decode: invalid Tektronix curve offsets (data=%d post=%d stop=%d end=%d)",
			dataStart, postchargeStart, postchargeStop, endOfBuffer)
	}
	if curveOffset+endOfBuffer > size {
		return core.Capture{}, fmt.Errorf("decode: Tektronix curve buffer exceeds file")
	}
	validBytes := postchargeStart - dataStart
	if validBytes < 0 || validBytes%bpp != 0 {
		return core.Capture{}, fmt.Errorf("decode: invalid Tektronix valid-data byte range")
	}
	n := validBytes / bpp
	if n <= 0 {
		return core.Capture{}, fmt.Errorf("decode: Tektronix waveform has no valid samples")
	}
	if n > opts.maxPoints() {
		return core.Capture{}, fmt.Errorf("%w: Tektronix waveform has %d points (limit %d)", core.ErrTooManyPoints, n, opts.maxPoints())
	}
	if impSizePoints > 0 && n > impSizePoints {
		return core.Capture{}, fmt.Errorf("decode: Tektronix valid points %d exceed implicit dimension size %d", n, impSizePoints)
	}
	_ = prechargeStart

	raw := make([]byte, validBytes)
	if _, err := r.ReadAt(raw, curveOffset+dataStart); err != nil {
		return core.Capture{}, err
	}
	samples := make([]float32, n)
	for i := int64(0); i < n; i++ {
		v, err := decodeSample(raw[i*bpp:(i+1)*bpp], formatBPP, signed, isFloat, order)
		if err != nil {
			return core.Capture{}, err
		}
		samples[i] = float32(expScale*v + expOffset)
	}
	startTime := impOffset + float64(dataStart/bpp)*impScale
	w := core.Waveform{Samples: samples, SampleRate: 1 / impScale, StartTime: startTime}
	if err := w.Validate(); err != nil {
		return core.Capture{}, err
	}
	channel := opts.Channel
	if channel == "" {
		channel = label
	}
	if channel == "" {
		channel = "CH1"
	}
	cap := core.Capture{
		Source: core.SourceInfo{
			Kind: core.SourceFile, Vendor: "Tektronix", FileFormat: "tektronix-" + strings.ToLower(version[4:]),
		},
		Records: []core.Record{{
			Channel: channel, SegmentIndex: 0, Waveform: w, Acquisition: "normal",
		}},
	}
	if err := cap.Validate(); err != nil {
		return cap, err
	}
	return cap, nil
}

func formatInfo(format int32) (bpp int64, signed, isFloat, ok bool) {
	switch format {
	case 0:
		return 2, true, false, true // int16
	case 1:
		return 4, true, false, true // int32
	case 2:
		return 4, false, false, true // uint32
	case 3:
		return 8, false, false, true // uint64
	case 4:
		return 4, true, true, true // float32
	case 5:
		return 8, true, true, true // float64
	case 6:
		return 1, false, false, true // uint8
	case 7:
		return 1, true, false, true // int8
	default:
		return 0, false, false, false
	}
}

func decodeSample(b []byte, bpp int64, signed, isFloat bool, order binary.ByteOrder) (float64, error) {
	switch bpp {
	case 1:
		if signed {
			return float64(int8(b[0])), nil
		}
		return float64(b[0]), nil
	case 2:
		u := order.Uint16(b)
		if isFloat {
			return float64(math.Float32frombits(uint32(u))), nil // not expected for 2 bytes
		}
		if signed {
			return float64(int16(u)), nil
		}
		return float64(u), nil
	case 4:
		u := order.Uint32(b)
		if isFloat {
			return float64(math.Float32frombits(u)), nil
		}
		if signed {
			return float64(int32(u)), nil
		}
		return float64(u), nil
	case 8:
		u := order.Uint64(b)
		if isFloat {
			return math.Float64frombits(u), nil
		}
		if signed {
			return float64(int64(u)), nil
		}
		return float64(u), nil
	default:
		return 0, fmt.Errorf("decode: unsupported Tektronix sample width %d", bpp)
	}
}

func cString(b []byte) string {
	i := 0
	for i < len(b) && b[i] != 0 {
		i++
	}
	return strings.TrimSpace(string(b[:i]))
}
