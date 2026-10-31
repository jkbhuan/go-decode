package core

import "errors"

// Common waveform-source / file-format errors. They are wrapped by the
// concrete reader with context; callers should use errors.Is.
var (
	ErrUnknownFormat              = errors.New("decode: unknown or unverified waveform file format")
	ErrUnsupportedFormatVersion   = errors.New("decode: unsupported waveform file format version")
	ErrUnsupportedAcquisitionType = errors.New("decode: unsupported acquisition type")
	ErrFileTooLarge               = errors.New("decode: file or payload exceeds the configured limit")
	ErrTooManyPoints              = errors.New("decode: waveform exceeds the configured point limit")
)
