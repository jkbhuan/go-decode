package core

import "fmt"

// Diagnostic severities. They are plain strings so that JSON output remains
// stable and easy to consume from other languages.
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Diagnostic describes a structured, machine-readable observation about the
// decode or a measurement. Diagnostics are not Go errors: malformed user
// configuration is returned as an error, while capture truncation, protocol
// violations and measures that cannot be observed are reported here so that
// successfully decoded parts can still be returned.
type Diagnostic struct {
	Code     string
	Message  string
	Severity string
	Time     *float64
}

// Stable diagnostic codes. Keep these values stable; downstream tools may key
// on them.
const (
	// Waveform / channel alignment.
	CodeWaveformPartialOverlap     = "waveform_partial_overlap"
	CodeWaveformInitialUnknown     = "waveform_initial_level_unknown"
	CodeWaveformStatic             = "waveform_signal_static"
	CodeWaveformNeverCrosses       = "waveform_never_crosses_threshold"
	CodeWaveformSamplingLow        = "waveform_sampling_low"
	CodeEdgeGlitchSuppressed       = "edge_glitch_suppressed"
	CodeTimingThresholdMissing     = "timing_threshold_missing"
	CodeTimingThresholdUnavailable = "timing_threshold_trace_unavailable"
	CodeTimingNotObservable        = "timing_not_observable"

	// I2C.
	CodeI2CNoStartCondition           = "i2c_no_start_condition"
	CodeI2CMissingACK                 = "i2c_missing_ack"
	CodeI2CMissingStop                = "i2c_missing_stop"
	CodeI2CTruncatedByte              = "i2c_truncated_byte"
	CodeI2CStartMidByte               = "i2c_start_mid_byte"
	CodeI2CStopMidByte                = "i2c_stop_mid_byte"
	CodeI2C10BitUnsupported           = "i2c_10bit_addressing_unsupported"
	CodeI2CNoTransactions             = "i2c_no_transactions"
	CodeI2CTimingThresholdUnavailable = "i2c_timing_threshold_unavailable"

	// SMI / MDIO Clause 22.
	CodeSMIInvalidST             = "smi_invalid_start_of_frame"
	CodeSMIInvalidOpcode         = "smi_invalid_opcode"
	CodeSMIInvalidTurnaround     = "smi_invalid_turnaround"
	CodeSMIReadTAUnverifiable    = "smi_read_turnaround_unverifiable"
	CodeSMITruncatedFrame        = "smi_truncated_frame"
	CodeSMIShortPreamble         = "smi_preamble_short"
	CodeSMINoFrames              = "smi_no_frames"
	CodeSMIReadTimingUnsupported = "smi_read_direction_timing_unsupported"
)

func NewDiagnostic(code, severity string, t *float64, format string, args ...any) Diagnostic {
	return Diagnostic{
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
		Severity: severity,
		Time:     t,
	}
}

func timePtr(t float64) *float64 {
	return &t
}
