package smi

import (
	"fmt"

	"github.com/jkbhuan/go-decode/core"
)

// ValidationStatus expresses how strongly a waveform-only observation can
// prove a protocol field.
type ValidationStatus uint8

const (
	StatusUnknown ValidationStatus = iota
	StatusValid
	StatusInvalid
	StatusUnverifiable
)

// String implements fmt.Stringer.
func (s ValidationStatus) String() string {
	switch s {
	case StatusValid:
		return "valid"
	case StatusInvalid:
		return "invalid"
	case StatusUnverifiable:
		return "unverifiable"
	default:
		return "unknown"
	}
}

// Frame is one IEEE 802.3 Clause 22 management frame. Start/End are in
// seconds on the instrument time base; with a preamble, Start is the first of
// the counted preamble bits, otherwise it is the first ST bit. End is the last
// captured frame bit (or the last captured bit of a truncated frame).
//
// Frame format reference: IEEE Std 802.3-2022, Clause 22.2.4.5 and Table 22-12
// (PRE, ST=01, OP, PHYAD, REGAD, TA, DATA); MDIO is sampled on the MDC rising
// edge. Turnaround behaviour is described in 22.2.4.5.7; the voltage-only
// limitation for the read-direction Z bit follows from the fact that a high
// level cannot prove a high-impedance driver state.
//
// BitsReceived counts the 32 frame bits (ST+OP+PHYAD+REGAD+TA+DATA) that were
// actually captured, excluding preamble bits. Complete is true only when all
// 32 bits were received. DataValid is true only when the full 16-bit DATA
// field was received; this distinguishes a genuinely observed zero from a
// truncated frame whose Data field is merely zero-valued.
type Frame struct {
	Direction    core.Direction
	PHYAddr      uint8
	RegAddr      uint8
	Data         uint16
	OpCode       uint8
	PreambleBits int
	Turnaround   ValidationStatus
	Start        float64
	End          float64
	Complete     bool
	DataValid    bool
	BitsReceived int
	Timings      []core.Timing
	Diagnostics  []core.Diagnostic
}

// Options configures Clause 22 MDIO decoding and timing.
//
// MDCThreshold and MDIOThreshold are required. TimingThreshold is optional: it
// defines the electrical levels at which MDC and MDIO timing is measured. If it
// is absent, decoding still works but no timing values are produced (a
// diagnostic explains why), because IEEE 802.3 does not prescribe timing
// thresholds that apply to every PHY/STA implementation.
//
// AllowPreambleSuppression permits frames that start directly with ST without
// 32 preamble ones (802.3 22.2.4.5.2). MinPreambleBits defaults to 32 when it
// is zero.
type Options struct {
	MDCThreshold             core.LogicThreshold
	MDIOThreshold            core.LogicThreshold
	TimingThreshold          *core.LogicThreshold
	GlitchWidth              float64
	AllowPreambleSuppression bool
	MinPreambleBits          int
}

// Result is the complete result of one Decode call.
//
// Frames preserves the decoded management frames. Timings is the same timing
// candidates flattened across all frames (core.Timing.FrameIndex identifies the
// frame). Diagnostics contains channel-level diagnostics plus all frame
// diagnostics. No JSON encoding is performed by this package.
type Result struct {
	Frames      []Frame
	Timings     []core.Timing
	Diagnostics []core.Diagnostic
}

func (o Options) validate() error {
	if err := o.MDCThreshold.Validate(); err != nil {
		return fmt.Errorf("decode: SMI MDC threshold: %w", err)
	}
	if err := o.MDIOThreshold.Validate(); err != nil {
		return fmt.Errorf("decode: SMI MDIO threshold: %w", err)
	}
	if o.MDCThreshold == (core.LogicThreshold{}) {
		return fmt.Errorf("decode: SMI MDC threshold is not configured")
	}
	if o.MDIOThreshold == (core.LogicThreshold{}) {
		return fmt.Errorf("decode: SMI MDIO threshold is not configured")
	}
	if !core.IsFinite(o.GlitchWidth) || o.GlitchWidth < 0 {
		return fmt.Errorf("decode: SMI glitch width must be a non-negative finite duration")
	}
	if o.MinPreambleBits < 0 {
		return fmt.Errorf("decode: SMI minimum preamble bits must be >= 0")
	}
	if o.TimingThreshold != nil {
		if err := o.TimingThreshold.Validate(); err != nil {
			return fmt.Errorf("decode: SMI timing threshold: %w", err)
		}
		if *o.TimingThreshold == (core.LogicThreshold{}) {
			return fmt.Errorf("decode: SMI timing threshold is zero and therefore not configured")
		}
	}
	return nil
}

func (o Options) minPreamble() int {
	if o.MinPreambleBits > 0 {
		return o.MinPreambleBits
	}
	return 32
}

// Decode decodes IEEE 802.3 Clause 22 (MDC/MDIO) management frames and
// returns a strongly typed result. Access frames via result.Frames, timing
// candidates via result.Timings, and cursor coordinates via core.Timing.X1/X2.
func Decode(mdc, mdio core.Waveform, opts Options) (Result, error) {
	if err := opts.validate(); err != nil {
		return Result{}, err
	}
	if err := mdc.Validate(); err != nil {
		return Result{}, fmt.Errorf("decode: MDC: %w", err)
	}
	if err := mdio.Validate(); err != nil {
		return Result{}, fmt.Errorf("decode: MDIO: %w", err)
	}

	cmdc, cmdio, alignDiags, err := core.AlignWaveforms(mdc, mdio)
	if err != nil {
		return Result{}, err
	}
	mdcTrace, err := core.ExtractLogicTraceOnChannel(cmdc, opts.MDCThreshold, opts.GlitchWidth, "MDC")
	if err != nil {
		return Result{}, err
	}
	mdioTrace, err := core.ExtractLogicTraceOnChannel(cmdio, opts.MDIOThreshold, opts.GlitchWidth, "MDIO")
	if err != nil {
		return Result{}, err
	}

	res := Result{Diagnostics: append([]core.Diagnostic{}, alignDiags...)}
	res.Diagnostics = append(res.Diagnostics, mdcTrace.Diagnostics...)
	res.Diagnostics = append(res.Diagnostics, mdioTrace.Diagnostics...)

	var mdcRises []mdcSample
	for i, e := range mdcTrace.Edges {
		if e.Kind == core.EdgeRising {
			mdcRises = append(mdcRises, mdcSample{time: e.Time, ordinal: i})
		}
	}

	if opts.TimingThreshold == nil {
		res.Diagnostics = append(res.Diagnostics, newDiagnostic(
			core.CodeTimingThresholdMissing, core.SeverityInfo, nil,
			"SMI/MDIO timing values omitted: no TimingThreshold was supplied for MDC/MDIO electrical levels"))
	}

	p := &smiParser{opts: opts, result: &res}
	for _, rise := range mdcRises {
		high, ok := mdioTrace.LevelAt(rise.time)
		if !ok {
			continue
		}
		p.feed(high, rise)
	}
	p.finish()

	if opts.TimingThreshold != nil {
		mdcTiming, err := core.ExtractLogicTraceOnChannel(cmdc, *opts.TimingThreshold, opts.GlitchWidth, "MDC")
		if err != nil {
			return Result{}, err
		}
		mdioTiming, err := core.ExtractLogicTraceOnChannel(cmdio, *opts.TimingThreshold, opts.GlitchWidth, "MDIO")
		if err != nil {
			return Result{}, err
		}
		// IEEE 802.3 22.3 defines HIGH time as the interval above Vih(min)
		// and LOW time as the interval below Vil(max). With a hysteresis
		// trace a rising edge is at Vih and a falling edge at Vil, which would
		// add the transition slope to HIGH and subtract it from LOW. Use
		// single-threshold traces for those two clock metrics.
		mdcHighTiming, err := core.ExtractLogicTraceOnChannel(cmdc, core.SingleThreshold(opts.TimingThreshold.High), opts.GlitchWidth, "MDC")
		if err != nil {
			return Result{}, err
		}
		mdcLowTiming, err := core.ExtractLogicTraceOnChannel(cmdc, core.SingleThreshold(opts.TimingThreshold.Low), opts.GlitchWidth, "MDC")
		if err != nil {
			return Result{}, err
		}
		res.Diagnostics = append(res.Diagnostics, mdcTiming.Diagnostics...)
		res.Diagnostics = append(res.Diagnostics, mdioTiming.Diagnostics...)
		if !mdcTiming.InitialKnown || !mdioTiming.InitialKnown || !mdcHighTiming.InitialKnown || !mdcLowTiming.InitialKnown {
			res.Diagnostics = append(res.Diagnostics, newDiagnostic(
				core.CodeTimingThresholdUnavailable, core.SeverityWarning, nil,
				"SMI/MDIO timing reference levels could not be established on one or both channels; timing values omitted"))
		} else {
			computeSMITimings(res.Frames, p.metas, mdcTiming, mdioTiming, mdcHighTiming, mdcLowTiming)
		}
	}

	if len(res.Frames) == 0 {
		res.Diagnostics = append(res.Diagnostics, newDiagnostic(
			core.CodeSMINoFrames, core.SeverityWarning, nil,
			"no valid Clause 22 management frame was observed"))
	}
	res.aggregate()
	return res, nil
}

func (r *Result) aggregate() {
	timings := make([]core.Timing, 0)
	for i := range r.Frames {
		for j := range r.Frames[i].Timings {
			r.Frames[i].Timings[j].TransactionIndex = -1
			r.Frames[i].Timings[j].FrameIndex = i
			timings = append(timings, r.Frames[i].Timings[j])
		}
		r.Diagnostics = append(r.Diagnostics, r.Frames[i].Diagnostics...)
	}
	r.Timings = timings
}

// ---------------------------------------------------------------------------
// Internal parser
// ---------------------------------------------------------------------------

type mdcSample struct {
	time    float64
	ordinal int
}

type smiBitMeta struct {
	phase      string
	bitIndex   int // 0..31 within ST..DATA
	mdcRise    float64
	mdcOrdinal int
}

type smiFrameMeta struct {
	bits []smiBitMeta
}

type smiParser struct {
	opts   Options
	result *Result
	metas  []smiFrameMeta

	inFrame  bool
	expectST bool // waiting for the second ST bit (the 1)
	preamble int
	start    float64
	payload  []bool // OP..DATA, 30 bits
	bitsRecv int
	bitMeta  []smiBitMeta
	frameEnd float64

	highRun      int
	highRunStart float64
	highTimes    []float64
	shortWarned  bool
}

func (p *smiParser) feed(bit bool, rise mdcSample) {
	if !p.inFrame {
		if bit {
			if p.highRun == 0 {
				p.highRunStart = rise.time
				p.highTimes = p.highTimes[:0]
			}
			p.highRun++
			p.highTimes = append(p.highTimes, rise.time)
			maxKeep := p.opts.minPreamble()
			if len(p.highTimes) > maxKeep {
				p.highTimes = p.highTimes[len(p.highTimes)-maxKeep:]
			}
			return
		}
		// A zero in the idle state is the first ST bit (or noise).
		minPre := p.opts.minPreamble()
		switch {
		case p.highRun >= minPre:
			p.startFrame(p.highRun, rise, true)
		case p.opts.AllowPreambleSuppression:
			p.startFrame(0, rise, true)
		default:
			if p.highRun > 0 && !p.shortWarned {
				p.shortWarned = true
				p.result.Diagnostics = append(p.result.Diagnostics, newDiagnostic(
					core.CodeSMIShortPreamble, core.SeverityInfo, timePtr(rise.time),
					"observed %d preamble one(s), fewer than the required %d; start-of-frame candidate ignored",
					p.highRun, minPre))
			}
			p.highRun = 0
			p.highTimes = p.highTimes[:0]
		}
		return
	}

	if p.expectST {
		p.recordBit("st", 1, rise)
		p.bitsRecv++
		if !bit {
			p.result.Diagnostics = append(p.result.Diagnostics, newDiagnostic(
				core.CodeSMIInvalidST, core.SeverityWarning, timePtr(rise.time),
				"start-of-frame second bit is 0; expected <01>, frame abandoned"))
			p.abortFrame()
			return
		}
		p.expectST = false
		p.frameEnd = rise.time
		return
	}

	p.payload = append(p.payload, bit)
	p.bitsRecv++
	pos := len(p.payload) - 1
	p.recordBit(smiPhase(pos), 2+pos, rise)
	p.frameEnd = rise.time
	if len(p.payload) == 30 {
		p.completeFrame()
	}
}

func (p *smiParser) startFrame(preamble int, rise mdcSample, firstZero bool) {
	p.inFrame = true
	p.expectST = true
	// Report at most the standard/configurable preamble length. Additional
	// leading ones are indistinguishable idle bits, not part of PRE.
	p.preamble = preamble
	if maxPre := p.opts.minPreamble(); p.preamble > maxPre {
		p.preamble = maxPre
	}
	p.payload = p.payload[:0]
	p.bitsRecv = 0
	p.bitMeta = p.bitMeta[:0]
	p.frameEnd = rise.time

	if preamble > 0 {
		// Report the standard preamble length, using the most recent ones
		// immediately before ST (earlier ones are indistinguishable idle).
		keep := p.opts.minPreamble()
		if preamble < keep {
			keep = preamble
		}
		startIdx := len(p.highTimes) - keep
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx < len(p.highTimes) {
			p.start = p.highTimes[startIdx]
		} else {
			p.start = p.highRunStart
		}
	} else {
		p.start = rise.time
	}
	if firstZero {
		p.recordBit("st", 0, rise)
		p.bitsRecv = 1
	}
	p.highRun = 0
	p.highTimes = p.highTimes[:0]
}

func (p *smiParser) recordBit(phase string, bitIndex int, rise mdcSample) {
	p.bitMeta = append(p.bitMeta, smiBitMeta{
		phase:      phase,
		bitIndex:   bitIndex,
		mdcRise:    rise.time,
		mdcOrdinal: rise.ordinal,
	})
}

func (p *smiParser) abortFrame() {
	p.inFrame = false
	p.payload = p.payload[:0]
	p.bitMeta = p.bitMeta[:0]
	p.bitsRecv = 0
	p.highRun = 0
	p.highTimes = p.highTimes[:0]
}

func (p *smiParser) completeFrame() {
	payload := p.payload
	op := bitsToUint(payload[0:2])
	phy := uint8(bitsToUint(payload[2:7]))
	reg := uint8(bitsToUint(payload[7:12]))
	ta1, ta2 := payload[12], payload[13]
	data := uint16(bitsToUint(payload[14:30]))

	frame := Frame{
		PHYAddr:      phy,
		RegAddr:      reg,
		Data:         data,
		OpCode:       uint8(op),
		PreambleBits: p.preamble,
		Start:        p.start,
		End:          p.frameEnd,
		Complete:     true,
		DataValid:    true,
		BitsReceived: 32,
	}

	switch op {
	case 1:
		frame.Direction = core.DirectionWrite
		if ta1 && !ta2 {
			frame.Turnaround = StatusValid
		} else {
			frame.Turnaround = StatusInvalid
			frame.Diagnostics = append(frame.Diagnostics, newDiagnostic(
				core.CodeSMIInvalidTurnaround, core.SeverityWarning, timePtr(p.frameEnd),
				"write-frame turnaround is %d%d, expected 10", b2i(ta1), b2i(ta2)))
		}
	case 2:
		frame.Direction = core.DirectionRead
		if ta1 && !ta2 {
			// A high level on the first TA bit is consistent with the PHY
			// releasing MDIO and the pull-up holding it high, but a voltage
			// waveform cannot prove the high-impedance state.
			frame.Turnaround = StatusUnverifiable
			frame.Diagnostics = append(frame.Diagnostics, newDiagnostic(
				core.CodeSMIReadTAUnverifiable, core.SeverityInfo, timePtr(p.frameEnd),
				"read turnaround first bit is high; this is consistent with the PHY releasing MDIO, but high-Z cannot be proven from an analog voltage waveform"))
		} else {
			frame.Turnaround = StatusInvalid
			frame.Diagnostics = append(frame.Diagnostics, newDiagnostic(
				core.CodeSMIInvalidTurnaround, core.SeverityWarning, timePtr(p.frameEnd),
				"read-frame turnaround is %d%d, expected a pulled-up 1 followed by PHY-driven 0", b2i(ta1), b2i(ta2)))
		}
	default:
		frame.Direction = core.DirectionUnknown
		frame.Turnaround = StatusUnknown
		frame.Diagnostics = append(frame.Diagnostics, newDiagnostic(
			core.CodeSMIInvalidOpcode, core.SeverityWarning, timePtr(p.frameEnd),
			"illegal management-frame opcode %02b (Clause 22 defines 01=write, 10=read)", op))
	}

	p.result.Frames = append(p.result.Frames, frame)
	p.metas = append(p.metas, smiFrameMeta{
		bits: append([]smiBitMeta(nil), p.bitMeta...),
	})

	last := payload[len(payload)-1]
	p.inFrame = false
	p.payload = p.payload[:0]
	p.bitMeta = p.bitMeta[:0]
	p.bitsRecv = 0
	if last {
		p.highRun = 1
		p.highRunStart = p.frameEnd
		p.highTimes = append(p.highTimes[:0], p.frameEnd)
	} else {
		p.highRun = 0
		p.highTimes = p.highTimes[:0]
	}
}

func (p *smiParser) finish() {
	if !p.inFrame {
		return
	}
	if p.expectST {
		// Only the first ST bit (the 0) was captured. A lone zero is not yet
		// a management frame; reporting one would fabricate a frame start.
		p.result.Diagnostics = append(p.result.Diagnostics, newDiagnostic(
			core.CodeSMITruncatedFrame, core.SeverityWarning, timePtr(p.frameEnd),
			"capture ended after the first start-of-frame bit but before the second; no frame is reported"))
		p.inFrame = false
		p.payload = p.payload[:0]
		p.bitMeta = p.bitMeta[:0]
		p.bitsRecv = 0
		return
	}
	// Capture ended in the middle of a frame: keep only fields that were
	// fully received, and never mark the frame complete.
	payload := p.payload
	frame := Frame{
		PreambleBits: p.preamble,
		Start:        p.start,
		End:          p.frameEnd,
		Complete:     false,
		BitsReceived: p.bitsRecv,
	}
	if len(payload) >= 2 {
		frame.OpCode = uint8(bitsToUint(payload[0:2]))
		switch frame.OpCode {
		case 1:
			frame.Direction = core.DirectionWrite
		case 2:
			frame.Direction = core.DirectionRead
		}
	}
	if len(payload) >= 7 {
		frame.PHYAddr = uint8(bitsToUint(payload[2:7]))
	}
	if len(payload) >= 12 {
		frame.RegAddr = uint8(bitsToUint(payload[7:12]))
	}
	if len(payload) >= 14 {
		ta1, ta2 := payload[12], payload[13]
		switch frame.Direction {
		case core.DirectionWrite:
			if ta1 && !ta2 {
				frame.Turnaround = StatusValid
			} else {
				frame.Turnaround = StatusInvalid
			}
		case core.DirectionRead:
			if ta1 && !ta2 {
				frame.Turnaround = StatusUnverifiable
			} else {
				frame.Turnaround = StatusInvalid
			}
		default:
			frame.Turnaround = StatusUnknown
		}
	}
	if len(payload) >= 30 {
		frame.Data = uint16(bitsToUint(payload[14:30]))
		frame.DataValid = true
	}
	frame.Diagnostics = append(frame.Diagnostics, newDiagnostic(
		core.CodeSMITruncatedFrame, core.SeverityWarning, timePtr(p.frameEnd),
		"capture ended after %d of 32 frame bits; fields that were not fully received are not reported as valid", p.bitsRecv))
	p.result.Frames = append(p.result.Frames, frame)
	p.metas = append(p.metas, smiFrameMeta{
		bits: append([]smiBitMeta(nil), p.bitMeta...),
	})
	p.inFrame = false
}

func smiPhase(pos int) string {
	switch {
	case pos < 2:
		return "op"
	case pos < 7:
		return "phyad"
	case pos < 12:
		return "regad"
	case pos < 14:
		return "turnaround"
	default:
		return "data"
	}
}

func bitsToUint(bits []bool) uint32 {
	var v uint32
	for _, b := range bits {
		v <<= 1
		if b {
			v |= 1
		}
	}
	return v
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func newDiagnostic(code, severity string, t *float64, format string, args ...any) core.Diagnostic {
	return core.NewDiagnostic(code, severity, t, format, args...)
}

func timePtr(t float64) *float64 { return &t }
