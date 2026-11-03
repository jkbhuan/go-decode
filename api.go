// Package decode is the public facade for protocol decoding and waveform
// adaptation. Bus algorithms live in the bus/* subpackages, common waveform
// types in core, and vendor formats in scope/*. Decode performs no instrument
// or filesystem I/O; live SCPI acquisition is the caller's responsibility and
// file opening is in the fileio subpackage. The root package never imports
// VISA or any application/UI dependency.
package decode

import (
	busi2c "github.com/jkbhuan/go-decode/bus/i2c"
	bussmi "github.com/jkbhuan/go-decode/bus/smi"
	"github.com/jkbhuan/go-decode/core"
)

// Core types.
type (
	Waveform             = core.Waveform
	Timing               = core.Timing
	TimingName           = core.TimingName
	TimingFilter         = core.TimingFilter
	Direction            = core.Direction
	Diagnostic           = core.Diagnostic
	LogicThreshold       = core.LogicThreshold
	LogicTrace           = core.LogicTrace
	Edge                 = core.Edge
	EdgeKind             = core.EdgeKind
	SelectionMode        = core.SelectionMode
	FrequencyMeasurement = core.FrequencyMeasurement

	Capture    = core.Capture
	Record     = core.Record
	SourceInfo = core.SourceInfo
	SourceKind = core.SourceKind
)

// Core constants and errors.
const (
	SeverityInfo    = core.SeverityInfo
	SeverityWarning = core.SeverityWarning
	SeverityError   = core.SeverityError

	DirectionUnknown = core.DirectionUnknown
	DirectionWrite   = core.DirectionWrite
	DirectionRead    = core.DirectionRead

	EdgeRising  = core.EdgeRising
	EdgeFalling = core.EdgeFalling

	SelectFirst = core.SelectFirst
	SelectMin   = core.SelectMin
	SelectMax   = core.SelectMax

	SourceSCPI      = core.SourceSCPI
	SourceFile      = core.SourceFile
	SourceSynthetic = core.SourceSynthetic

	TimingHDSTA     = core.TimingHDSTA
	TimingSUSTA     = core.TimingSUSTA
	TimingSUSTO     = core.TimingSUSTO
	TimingBUF       = core.TimingBUF
	TimingSUDAT     = core.TimingSUDAT
	TimingHDDAT     = core.TimingHDDAT
	TimingTHIGH     = core.TimingTHIGH
	TimingTLOW      = core.TimingTLOW
	TimingMDCPeriod = core.TimingMDCPeriod
	TimingMDCHigh   = core.TimingMDCHigh
	TimingMDCLow    = core.TimingMDCLow
	TimingSUMDIO    = core.TimingSUMDIO
	TimingHDMDIO    = core.TimingHDMDIO
)

const (
	CodeWaveformPartialOverlap        = core.CodeWaveformPartialOverlap
	CodeWaveformInitialUnknown        = core.CodeWaveformInitialUnknown
	CodeWaveformStatic                = core.CodeWaveformStatic
	CodeWaveformNeverCrosses          = core.CodeWaveformNeverCrosses
	CodeWaveformSamplingLow           = core.CodeWaveformSamplingLow
	CodeEdgeGlitchSuppressed          = core.CodeEdgeGlitchSuppressed
	CodeTimingThresholdMissing        = core.CodeTimingThresholdMissing
	CodeTimingThresholdUnavailable    = core.CodeTimingThresholdUnavailable
	CodeTimingNotObservable           = core.CodeTimingNotObservable
	CodeI2CNoStartCondition           = core.CodeI2CNoStartCondition
	CodeI2CMissingACK                 = core.CodeI2CMissingACK
	CodeI2CMissingStop                = core.CodeI2CMissingStop
	CodeI2CTruncatedByte              = core.CodeI2CTruncatedByte
	CodeI2CStartMidByte               = core.CodeI2CStartMidByte
	CodeI2CStopMidByte                = core.CodeI2CStopMidByte
	CodeI2C10BitUnsupported           = core.CodeI2C10BitUnsupported
	CodeI2CNoTransactions             = core.CodeI2CNoTransactions
	CodeI2CTimingThresholdUnavailable = core.CodeI2CTimingThresholdUnavailable
	CodeSMIInvalidST                  = core.CodeSMIInvalidST
	CodeSMIInvalidOpcode              = core.CodeSMIInvalidOpcode
	CodeSMIInvalidTurnaround          = core.CodeSMIInvalidTurnaround
	CodeSMIReadTAUnverifiable         = core.CodeSMIReadTAUnverifiable
	CodeSMITruncatedFrame             = core.CodeSMITruncatedFrame
	CodeSMIShortPreamble              = core.CodeSMIShortPreamble
	CodeSMINoFrames                   = core.CodeSMINoFrames
	CodeSMIReadTimingUnsupported      = core.CodeSMIReadTimingUnsupported
)

var (
	ErrEmptyWaveform       = core.ErrEmptyWaveform
	ErrInvalidSampleRate   = core.ErrInvalidSampleRate
	ErrInvalidStartTime    = core.ErrInvalidStartTime
	ErrNonFiniteSample     = core.ErrNonFiniteSample
	ErrInvalidTimeAxis     = core.ErrInvalidTimeAxis
	ErrIndexOutOfRange     = core.ErrIndexOutOfRange
	ErrNoTimeOverlap       = core.ErrNoTimeOverlap
	ErrInsufficientOverlap = core.ErrInsufficientOverlap

	ErrUnknownFormat              = core.ErrUnknownFormat
	ErrUnsupportedFormatVersion   = core.ErrUnsupportedFormatVersion
	ErrUnsupportedAcquisitionType = core.ErrUnsupportedAcquisitionType
	ErrFileTooLarge               = core.ErrFileTooLarge
	ErrTooManyPoints              = core.ErrTooManyPoints
)

// Core functions.
func NewWaveform(samples []float32, sampleRate, startTime float64) (Waveform, error) {
	return core.NewWaveform(samples, sampleRate, startTime)
}

func AlignWaveforms(a, b Waveform) (Waveform, Waveform, []Diagnostic, error) {
	return core.AlignWaveforms(a, b)
}

func ExtractLogicTrace(w Waveform, threshold LogicThreshold, glitchWidth float64) (LogicTrace, error) {
	return core.ExtractLogicTrace(w, threshold, glitchWidth)
}

func ExtractLogicTraceOnChannel(w Waveform, threshold LogicThreshold, glitchWidth float64, channel string) (LogicTrace, error) {
	return core.ExtractLogicTraceOnChannel(w, threshold, glitchWidth, channel)
}

func SingleThreshold(v float64) LogicThreshold { return core.SingleThreshold(v) }

func SelectTiming(all []Timing, filter TimingFilter, mode SelectionMode) (Timing, bool) {
	return core.SelectTiming(all, filter, mode)
}

func SelectTimings(all []Timing, filter TimingFilter) []Timing {
	return core.SelectTimings(all, filter)
}

// I2C bus types.
type (
	I2COptions     = busi2c.Options
	I2CResult      = busi2c.Result
	I2CFrame       = busi2c.Frame
	I2CTransaction = busi2c.Transaction
	I2CByte        = busi2c.Byte
	ACKStatus      = busi2c.ACKStatus
)

const (
	ACKUnknown = busi2c.ACKUnknown
	ACK        = busi2c.ACK
	NACK       = busi2c.NACK
)

// I2C is the recommended I2C entry point.
func I2C(scl, sda Waveform, opts I2COptions) (I2CResult, error) {
	return busi2c.Decode(scl, sda, opts)
}

// DecodeI2C is kept for compatibility.
//
// Deprecated: use I2C.
func DecodeI2C(scl, sda Waveform, opts I2COptions) (I2CResult, error) {
	return I2C(scl, sda, opts)
}

// SMI bus types.
type (
	SMIOptions       = bussmi.Options
	SMIResult        = bussmi.Result
	SMIFrame         = bussmi.Frame
	ValidationStatus = bussmi.ValidationStatus
)

const (
	StatusUnknown      = bussmi.StatusUnknown
	StatusValid        = bussmi.StatusValid
	StatusInvalid      = bussmi.StatusInvalid
	StatusUnverifiable = bussmi.StatusUnverifiable
)

// SMI is the recommended IEEE 802.3 Clause 22 entry point.
func SMI(mdc, mdio Waveform, opts SMIOptions) (SMIResult, error) {
	return bussmi.Decode(mdc, mdio, opts)
}

// DecodeSMI is kept for compatibility.
//
// Deprecated: use SMI.
func DecodeSMI(mdc, mdio Waveform, opts SMIOptions) (SMIResult, error) {
	return SMI(mdc, mdio, opts)
}
