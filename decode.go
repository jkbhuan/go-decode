// Package decode is the public facade for vendor-independent bus decoding and
// waveform adaptation. It performs no instrument or filesystem I/O: callers
// must supply waveforms, response payloads, or caller-opened readers.
//
// Layers:
//   - core: Waveform, Capture, thresholds, edges, timing candidates,
//     diagnostics and channel alignment. It imports no bus, vendor or VISA code.
//   - bus/i2c and bus/smi: protocol state machines and timing.
//   - scope/keysight and scope/tektronix: pure payload and saved-file parsers;
//     they never send SCPI commands or open files.
//   - fileio: format routing over a caller-provided io.ReaderAt and raw-payload
//     metadata handling.
//
// Live SCPI acquisition is out of scope for this package; opening saved
// waveform files is handled by the fileio subpackage.
//
// The recommended bus entry points are decode.I2C and decode.SMI. The older
// DecodeI2C/DecodeSMI names remain as deprecated forwarding wrappers.
//
// Units: every time value is in seconds on the oscilloscope's horizontal time
// base, and every voltage is in volts. StartTime is the time of sample zero,
// never a Unix epoch value. All decode results are strongly typed Go structs;
// JSON serialization is the caller's responsibility.
//
// Supported protocols in the first release:
//   - I2C, 7-bit addressing, Standard/Fast/Fast-mode Plus terminology.
//   - IEEE 802.3 Clause 22 SMI/MDIO (PRE/ST/OP/PHYAD/REGAD/TA/DATA).
package decode
