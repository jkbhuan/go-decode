// Package fileio routes oscilloscope-saved waveform files to the vendor
// parsers and returns core.Capture values. It does not decode buses.
package fileio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jkbhuan/go-decode/core"
	"github.com/jkbhuan/go-decode/scope/keysight"
	"github.com/jkbhuan/go-decode/scope/tektronix"
)

// Errors re-exported for callers using errors.Is.
var (
	ErrUnknownFormat              = core.ErrUnknownFormat
	ErrUnsupportedFormatVersion   = core.ErrUnsupportedFormatVersion
	ErrUnsupportedAcquisitionType = core.ErrUnsupportedAcquisitionType
	ErrFileTooLarge               = core.ErrFileTooLarge
	ErrTooManyPoints              = core.ErrTooManyPoints
)

// Options limits file reads and supplies fallback hints. Hints are never used
// to override a conflicting file header.
type Options struct {
	VendorHint  string
	ModelHint   string
	ChannelHint string
	MaxBytes    int64
	MaxPoints   int64
}

func (o Options) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return 512 << 20
}

// Read parses a saved waveform file from an io.ReaderAt supplied by the caller.
// It does not open files, access instruments, or take ownership of the reader.
func Read(r io.ReaderAt, size int64, opts Options) (core.Capture, error) {
	if size <= 0 {
		return core.Capture{}, fmt.Errorf("%w: empty file", core.ErrUnknownFormat)
	}
	if size > opts.maxBytes() {
		return core.Capture{}, fmt.Errorf("%w: size %d > MaxBytes %d", core.ErrFileTooLarge, size, opts.maxBytes())
	}
	head := make([]byte, 16)
	if _, err := r.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return core.Capture{}, err
	}
	switch {
	case bytes.HasPrefix(head, []byte("AG")):
		return keysight.ReadBin(r, size, keysight.BinOptions{MaxBytes: opts.maxBytes(), MaxPoints: opts.MaxPoints})
	case bytes.HasPrefix(head[2:], []byte(":WFM#")):
		c, err := tektronix.ReadWFM(r, size, tektronix.WFMOptions{
			MaxBytes: opts.maxBytes(), MaxPoints: opts.MaxPoints, Channel: opts.ChannelHint,
		})
		if c.Source.Model == "" {
			c.Source.Model = opts.ModelHint
		}
		return c, err
	case bytes.HasPrefix(head, []byte{0x2b, 0x01, 0x01}):
		return core.Capture{}, fmt.Errorf("%w: Keysight saved .wfm format is not implemented; use Keysight .bin or SCPI data", core.ErrUnknownFormat)
	default:
		ext := ""
		if strings.HasPrefix(strings.ToLower(opts.VendorHint), "keysight") {
			ext = " (Keysight hint ignored because the file header is not AG10)"
		}
		return core.Capture{}, fmt.Errorf("%w: unrecognized header % X%s", core.ErrUnknownFormat, head[:min(8, len(head))], ext)
	}
}

// RawMetadata must be supplied explicitly for a raw SCPI payload. A raw
// payload alone has no trustworthy time or voltage scaling.
type RawMetadata struct {
	Preamble keysight.Preamble
	Encoding keysight.SampleEncoding
	Channel  string
	Source   core.SourceInfo
}

// ReadRaw restores a raw SCPI payload only when the caller explicitly supplies
// its preamble and sample encoding. The payload must already have its IEEE
// 488.2 block header removed.
func ReadRaw(payload []byte, meta RawMetadata) (core.Capture, error) {
	if meta.Channel == "" {
		meta.Channel = "CH1"
	}
	w, err := keysight.FormatWaveform(payload, meta.Preamble, meta.Encoding)
	if err != nil {
		return core.Capture{}, err
	}
	if meta.Source.Kind == "" {
		meta.Source.Kind = core.SourceFile
	}
	cap := core.Capture{
		Source: meta.Source,
		Records: []core.Record{{
			Channel: meta.Channel, SegmentIndex: 0, Waveform: w, Acquisition: "normal",
		}},
	}
	if err := cap.Validate(); err != nil {
		return cap, err
	}
	return cap, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
