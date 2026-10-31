package keysight

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/jkbhuan/go-decode/core"
)

// BinOptions limits saved-file reads.
type BinOptions struct {
	MaxBytes  int64
	MaxPoints int64
}

func (o BinOptions) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return 512 << 20
}

func (o BinOptions) maxPoints() int64 {
	if o.MaxPoints > 0 {
		return o.MaxPoints
	}
	return 100_000_000
}

// ReadBin parses a Keysight InfiniiVision saved binary (.bin, "AG10") file.
//
// The supported layout is the documented AG10 file header (12 bytes), waveform
// header (140 bytes), waveform data header (12 bytes), and buffer type 1
// (normal 32-bit float, volts). Multi-waveform files and segmented records are
// preserved as separate core.Records. Peak-detect/envelope and digital buffers
// are rejected explicitly.
func ReadBin(r io.ReaderAt, size int64, opts BinOptions) (core.Capture, error) {
	if size <= 0 || size > opts.maxBytes() {
		return core.Capture{}, fmt.Errorf("%w: size %d (limit %d)", core.ErrFileTooLarge, size, opts.maxBytes())
	}
	fileHeader := make([]byte, 12)
	if _, err := r.ReadAt(fileHeader, 0); err != nil {
		return core.Capture{}, fmt.Errorf("decode: read Keysight BIN header: %w", err)
	}
	if string(fileHeader[0:2]) != "AG" {
		return core.Capture{}, fmt.Errorf("%w: missing AG cookie", core.ErrUnknownFormat)
	}
	version := string(fileHeader[2:4])
	if version != "10" {
		return core.Capture{}, fmt.Errorf("%w: Keysight BIN version %q (only AG10 is implemented)", core.ErrUnsupportedFormatVersion, version)
	}
	var order binary.ByteOrder = binary.LittleEndian
	declaredSize := int64(order.Uint32(fileHeader[4:8]))
	if declaredSize != size {
		be := binary.ByteOrder(binary.BigEndian)
		if int64(be.Uint32(fileHeader[4:8])) == size {
			order = be
			declaredSize = size
		} else {
			return core.Capture{}, fmt.Errorf("decode: Keysight BIN size field %d does not match file size %d", declaredSize, size)
		}
	}
	waveformCount := order.Uint32(fileHeader[8:12])
	if waveformCount == 0 || int64(waveformCount) > opts.maxPoints() {
		return core.Capture{}, fmt.Errorf("decode: invalid Keysight BIN waveform count %d", waveformCount)
	}

	cap := core.Capture{Source: core.SourceInfo{
		Kind: core.SourceFile, Vendor: "Keysight", FileFormat: "keysight-bin-AG10",
	}}
	offset := int64(12)
	for wi := uint32(0); wi < waveformCount; wi++ {
		if offset+140 > size {
			return cap, fmt.Errorf("decode: truncated Keysight waveform header at offset %d", offset)
		}
		wh := make([]byte, 140)
		if _, err := r.ReadAt(wh, offset); err != nil {
			return cap, err
		}
		headerSize := int64(order.Uint32(wh[0:4]))
		waveformType := order.Uint32(wh[4:8])
		buffers := order.Uint32(wh[8:12])
		points := int64(order.Uint32(wh[12:16]))
		_ = order.Uint32(wh[16:20]) // count
		xInc := math.Float64frombits(order.Uint64(wh[32:40]))
		xOrigin := math.Float64frombits(order.Uint64(wh[40:48]))
		xUnits := order.Uint32(wh[48:52])
		yUnits := order.Uint32(wh[52:56])
		frame := cString(wh[88:112])
		label := cString(wh[112:128])
		segment := int(order.Uint32(wh[136:140]))

		if headerSize < 140 || offset+headerSize+12 > size {
			return cap, fmt.Errorf("decode: invalid Keysight waveform header size %d", headerSize)
		}
		if waveformType != 1 {
			return cap, fmt.Errorf("%w: Keysight waveform type %d (only normal analog type 1 is supported)", core.ErrUnsupportedAcquisitionType, waveformType)
		}
		if buffers != 1 {
			return cap, fmt.Errorf("%w: Keysight waveform has %d buffers", core.ErrUnsupportedAcquisitionType, buffers)
		}
		if points <= 0 || points > opts.maxPoints() {
			return cap, fmt.Errorf("%w: Keysight waveform has %d points (limit %d)", core.ErrTooManyPoints, points, opts.maxPoints())
		}
		if xInc <= 0 || !core.IsFinite(xInc) || !core.IsFinite(xOrigin) {
			return cap, fmt.Errorf("decode: invalid Keysight X scaling (xInc=%g xOrigin=%g)", xInc, xOrigin)
		}
		if xUnits != 2 || yUnits != 1 {
			return cap, fmt.Errorf("%w: Keysight units X=%d Y=%d (expected seconds/volts)", core.ErrUnsupportedAcquisitionType, xUnits, yUnits)
		}

		dataHeaderOffset := offset + headerSize
		dh := make([]byte, 12)
		if _, err := r.ReadAt(dh, dataHeaderOffset); err != nil {
			return cap, err
		}
		dataHeaderSize := int64(order.Uint32(dh[0:4]))
		bufferType := int16(order.Uint16(dh[4:6]))
		bpp := int64(int16(order.Uint16(dh[6:8])))
		bufferSize := int64(order.Uint32(dh[8:12]))
		if dataHeaderSize < 12 || dataHeaderOffset+dataHeaderSize > size {
			return cap, fmt.Errorf("decode: invalid Keysight data header size %d", dataHeaderSize)
		}
		if bufferType != 1 || bpp != 4 {
			return cap, fmt.Errorf("%w: Keysight buffer type %d bytes/point %d (only normal float32 is supported)", core.ErrUnsupportedAcquisitionType, bufferType, bpp)
		}
		wantBytes := points * 4
		if bufferSize != wantBytes {
			return cap, fmt.Errorf("decode: Keysight buffer size %d != points*4 (%d)", bufferSize, wantBytes)
		}
		dataOffset := dataHeaderOffset + dataHeaderSize
		if dataOffset+bufferSize > size {
			return cap, fmt.Errorf("decode: truncated Keysight waveform buffer")
		}
		data := make([]byte, bufferSize)
		if _, err := r.ReadAt(data, dataOffset); err != nil {
			return cap, err
		}
		samples := make([]float64, points)
		for i := int64(0); i < points; i++ {
			bits := order.Uint32(data[i*4 : i*4+4])
			samples[i] = float64(math.Float32frombits(bits))
		}
		w := core.Waveform{Samples: samples, SampleRate: 1 / xInc, StartTime: xOrigin}
		if err := w.Validate(); err != nil {
			return cap, fmt.Errorf("decode: Keysight waveform %q: %w", label, err)
		}
		channel := label
		if channel == "" {
			channel = fmt.Sprintf("CH%d", wi+1)
		}
		acq := "normal"
		cap.Records = append(cap.Records, core.Record{
			Channel: channel, SegmentIndex: segment, Waveform: w, Acquisition: acq,
		})
		if cap.Source.Model == "" {
			if before, _, ok := strings.Cut(frame, ":"); ok {
				cap.Source.Model = before
			} else if frame != "" {
				cap.Source.Model = frame
			}
		}
		offset = dataOffset + bufferSize
	}
	if err := cap.Validate(); err != nil {
		return cap, err
	}
	return cap, nil
}

func cString(b []byte) string {
	i := 0
	for i < len(b) && b[i] != 0 {
		i++
	}
	return strings.TrimSpace(string(b[:i]))
}
