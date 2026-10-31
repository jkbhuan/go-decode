package i2c

import (
	"fmt"
	"sort"

	"github.com/jkbhuan/go-decode/core"
)

// ACKStatus is the state of an acknowledged bit on an I2C bus.
//
// Protocol references: NXP UM10204 Rev. 7, Section 3.1.1 (START/STOP and
// repeated START), 3.1.3 (data validity: SDA stable while SCL is high),
// 3.1.4 (acknowledge) and 3.1.5 (clock stretching).
type ACKStatus uint8

const (
	ACKUnknown ACKStatus = iota
	ACK
	NACK
)

// String implements fmt.Stringer.
func (a ACKStatus) String() string {
	switch a {
	case ACK:
		return "ACK"
	case NACK:
		return "NACK"
	default:
		return "unknown"
	}
}

// Byte is one decoded byte (address or data) together with its ACK bit.
//
// Start is the time of the first SCL sampling edge of the byte; End is the
// time of the ACK sampling edge when it was captured, otherwise the last data
// bit's sampling edge. Times are seconds on the instrument time base.
type Byte struct {
	Value uint8
	Ack   ACKStatus
	Start float64
	End   float64
}

// Frame is the address/data phase following one START or repeated START.
//
// AddressBits is 7 for a normal 7-bit frame. If a 10-bit addressing prefix
// (11110xx) is observed, AddressBits is 0 and Address is 0: the frame is
// reported as unsupported rather than being misinterpreted as a 7-bit
// transaction. Direction is unknown in that case.
//
// Complete means that the frame's protocol content was fully captured and the
// frame ended at a byte boundary (by STOP or repeated START). It does not mean
// that the device acknowledged: a NACK is a valid, fully decoded outcome.
// Start is the START/repeated-START time; End is the STOP/repeated-START time,
// or the last observed activity when the record was truncated.
type Frame struct {
	TransactionIndex int
	Address          uint16
	AddressBits      uint8
	Direction        core.Direction
	AddressACK       ACKStatus
	Data             []Byte
	Start            float64
	End              float64
	HasStop          bool
	RepeatedStart    bool
	Complete         bool
}

// Transaction groups all frames between one START and its STOP. A combined
// register read (write address + data, repeated START, read address + data) is
// one transaction with two frames of different directions. No byte is
// interpreted as an internal register offset.
//
// Start/End use the instrument time base. Complete is true only when a STOP
// was captured and every frame is complete; a NACK does not by itself make a
// transaction incomplete.
type Transaction struct {
	Frames         []Frame
	Start          float64
	End            float64
	Complete       bool
	Timings        []core.Timing
	SCLFrequencies []core.FrequencyMeasurement
	Diagnostics    []core.Diagnostic
}

// Options configures I2C decoding and timing measurement.
//
// SCLThreshold and SDAThreshold are required. A zero value is treated as
// "not configured" and rejected.
//
// TimingThreshold is the pair of reference levels used for tHD;STA, tSU;DAT and
// the other NXP UM10204 timings. UM10204 Figure 38 uses V_IL = 0.3*VDD and
// V_IH = 0.7*VDD. If TimingThreshold is nil and VDD > 0 those levels are
// derived from VDD; if neither is supplied, decoding still works but no timing
// values are produced and a diagnostic explains why.
//
// GlitchWidth is an optional pulse-width filter applied before decoding; zero
// disables it. The same filter is used for the timing traces and is reported
// in a diagnostic when it changes the edge list.
type Options struct {
	SCLThreshold    core.LogicThreshold
	SDAThreshold    core.LogicThreshold
	TimingThreshold *core.LogicThreshold
	VDD             float64
	GlitchWidth     float64
}

// Result is the complete result of one DecodeI2C call.
//
// Transactions preserves the START..STOP grouping. Frames and Timings are the
// same decoded objects flattened for direct access; each carries a
// TransactionIndex so callers can still associate it with a transaction.
// Diagnostics contains channel-level diagnostics plus all transaction
// diagnostics. No JSON encoding is performed by this package.
type Result struct {
	Transactions   []Transaction
	Frames         []Frame
	Timings        []core.Timing
	SCLFrequencies []core.FrequencyMeasurement
	Diagnostics    []core.Diagnostic
}

func (o Options) validate() error {
	if err := o.SCLThreshold.Validate(); err != nil {
		return fmt.Errorf("decode: I2C SCL threshold: %w", err)
	}
	if err := o.SDAThreshold.Validate(); err != nil {
		return fmt.Errorf("decode: I2C SDA threshold: %w", err)
	}
	if o.SCLThreshold == (core.LogicThreshold{}) {
		return fmt.Errorf("decode: I2C SCL threshold is not configured")
	}
	if o.SDAThreshold == (core.LogicThreshold{}) {
		return fmt.Errorf("decode: I2C SDA threshold is not configured")
	}
	if !core.IsFinite(o.GlitchWidth) || o.GlitchWidth < 0 {
		return fmt.Errorf("decode: I2C glitch width must be a non-negative finite duration")
	}
	if o.VDD != 0 {
		if !core.IsFinite(o.VDD) || o.VDD <= 0 {
			return fmt.Errorf("decode: I2C VDD must be a positive finite voltage")
		}
	}
	if o.TimingThreshold != nil {
		if err := o.TimingThreshold.Validate(); err != nil {
			return fmt.Errorf("decode: I2C timing threshold: %w", err)
		}
		if *o.TimingThreshold == (core.LogicThreshold{}) {
			return fmt.Errorf("decode: I2C timing threshold is zero and therefore not configured")
		}
	}
	return nil
}

func (o Options) resolveTimingThreshold() (core.LogicThreshold, bool) {
	if o.TimingThreshold != nil {
		return *o.TimingThreshold, true
	}
	if o.VDD > 0 {
		return core.LogicThreshold{Low: 0.3 * o.VDD, High: 0.7 * o.VDD}, true
	}
	return core.LogicThreshold{}, false
}

// Decode decodes I2C 7-bit transactions from SCL and SDA waveforms and
// returns a strongly typed result. Access frames via result.Frames, timing
// candidates via result.Timings, and cursor coordinates via core.Timing.X1/X2.
func Decode(scl, sda core.Waveform, opts Options) (Result, error) {
	if err := opts.validate(); err != nil {
		return Result{}, err
	}
	if err := scl.Validate(); err != nil {
		return Result{}, fmt.Errorf("decode: SCL: %w", err)
	}
	if err := sda.Validate(); err != nil {
		return Result{}, fmt.Errorf("decode: SDA: %w", err)
	}

	cscl, csda, alignDiags, err := core.AlignWaveforms(scl, sda)
	if err != nil {
		return Result{}, err
	}

	sclTrace, err := core.ExtractLogicTraceOnChannel(cscl, opts.SCLThreshold, opts.GlitchWidth, "SCL")
	if err != nil {
		return Result{}, err
	}
	sdaTrace, err := core.ExtractLogicTraceOnChannel(csda, opts.SDAThreshold, opts.GlitchWidth, "SDA")
	if err != nil {
		return Result{}, err
	}

	res := Result{Diagnostics: append([]core.Diagnostic{}, alignDiags...)}
	res.Diagnostics = append(res.Diagnostics, sclTrace.Diagnostics...)
	res.Diagnostics = append(res.Diagnostics, sdaTrace.Diagnostics...)

	timingThreshold, haveTiming := opts.resolveTimingThreshold()
	if !haveTiming {
		res.Diagnostics = append(res.Diagnostics, newDiagnostic(
			core.CodeI2CTimingThresholdUnavailable, core.SeverityInfo, nil,
			"I2C timing values omitted: no VDD or TimingThreshold was supplied, so the UM10204 0.3*VDD/0.7*VDD reference levels are unknown"))
	}

	d := &i2cDecoder{
		scl:    sclTrace,
		sda:    sdaTrace,
		opts:   opts,
		result: &res,
	}
	d.run()

	if haveTiming {
		sclTiming, err := core.ExtractLogicTraceOnChannel(cscl, timingThreshold, opts.GlitchWidth, "SCL")
		if err != nil {
			return Result{}, err
		}
		sdaTiming, err := core.ExtractLogicTraceOnChannel(csda, timingThreshold, opts.GlitchWidth, "SDA")
		if err != nil {
			return Result{}, err
		}
		res.Diagnostics = append(res.Diagnostics, sclTiming.Diagnostics...)
		res.Diagnostics = append(res.Diagnostics, sdaTiming.Diagnostics...)
		if !sclTiming.InitialKnown || !sdaTiming.InitialKnown {
			res.Diagnostics = append(res.Diagnostics, newDiagnostic(
				core.CodeTimingThresholdUnavailable, core.SeverityWarning, nil,
				"I2C timing reference levels could not be established on one or both channels; timing values omitted"))
		} else {
			computeI2CTimings(res.Transactions, d.metas, sclTiming, sdaTiming)
		}
	}

	if len(res.Transactions) == 0 {
		res.Diagnostics = append(res.Diagnostics, newDiagnostic(
			core.CodeI2CNoTransactions, core.SeverityWarning, nil,
			"no START condition was observed, so no I2C transaction could be decoded"))
	}
	res.aggregate()
	return res, nil
}

// aggregate fills the flattened Frames/Timings/SCLFrequencies views and adds
// transaction diagnostics to the top-level result.
func (r *Result) aggregate() {
	frames := make([]Frame, 0)
	timings := make([]core.Timing, 0)
	freqs := make([]core.FrequencyMeasurement, 0)
	for i := range r.Transactions {
		tx := &r.Transactions[i]
		for j := range tx.Frames {
			tx.Frames[j].TransactionIndex = i
			frames = append(frames, tx.Frames[j])
		}
		for j := range tx.Timings {
			tx.Timings[j].TransactionIndex = i
			timings = append(timings, tx.Timings[j])
		}
		for j := range tx.SCLFrequencies {
			tx.SCLFrequencies[j].TransactionIndex = i
			freqs = append(freqs, tx.SCLFrequencies[j])
		}
		r.Diagnostics = append(r.Diagnostics, tx.Diagnostics...)
	}
	r.Frames = frames
	r.Timings = timings
	r.SCLFrequencies = freqs
}

// ---------------------------------------------------------------------------
// Internal decoder
// ---------------------------------------------------------------------------

type i2cFrameMeta struct {
	startTime    float64
	startOrdinal int // index of START SDA edge in the SCL/SDA edge list
}

type i2cBitMeta struct {
	frameIndex int
	byteIndex  int
	bitIndex   int
	phase      string
	direction  core.Direction
	sclRise    float64
	sclOrdinal int // index in the full SCL edge list
}

type i2cTxMeta struct {
	frames       []i2cFrameMeta
	bits         []i2cBitMeta
	startTime    float64
	stopTime     float64
	startOrdinal int
	stopOrdinal  int
	hasStop      bool
}

type mergedEdge struct {
	time    float64
	channel int // 0 = SCL, 1 = SDA
	kind    core.EdgeKind
	ordinal int
}

type i2cDecoder struct {
	scl    core.LogicTrace
	sda    core.LogicTrace
	opts   Options
	result *Result
	metas  []i2cTxMeta

	sclHigh bool
	sdaHigh bool

	inFrame bool
	tx      *Transaction
	txMeta  *i2cTxMeta
	frame   *Frame
	fmeta   *i2cFrameMeta

	bitCount      int
	byteValue     uint8
	byteStart     float64
	expectingAck  bool
	pendingAddr   bool
	pendingData   *Byte
	hasAddress    bool
	unsupported10 bool
	lastActivity  float64
}

func (d *i2cDecoder) run() {
	start := d.scl.KnownFrom
	if d.sda.KnownFrom > start {
		start = d.sda.KnownFrom
	}
	if !d.scl.InitialKnown || !d.sda.InitialKnown {
		return
	}
	d.sclHigh, _ = d.scl.LevelAt(start)
	d.sdaHigh, _ = d.sda.LevelAt(start)
	d.lastActivity = start

	events := make([]mergedEdge, 0, len(d.scl.Edges)+len(d.sda.Edges))
	for i, e := range d.scl.Edges {
		events = append(events, mergedEdge{time: e.Time, channel: 0, kind: e.Kind, ordinal: i})
	}
	for i, e := range d.sda.Edges {
		events = append(events, mergedEdge{time: e.Time, channel: 1, kind: e.Kind, ordinal: i})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].time != events[j].time {
			return events[i].time < events[j].time
		}
		return events[i].channel < events[j].channel
	})

	for _, ev := range events {
		if ev.time < start {
			continue
		}
		d.lastActivity = ev.time
		switch ev.channel {
		case 0:
			d.sclHigh = ev.kind == core.EdgeRising
			if ev.kind == core.EdgeRising {
				d.onSCLRising(ev)
			}
		case 1:
			d.sdaHigh = ev.kind == core.EdgeRising
			if d.sclHigh {
				if ev.kind == core.EdgeFalling {
					d.onStartOrRepeatedStart(ev)
				} else {
					d.onStop(ev)
				}
			}
		}
	}
	d.finishAtEnd()
}

func (d *i2cDecoder) onSCLRising(ev mergedEdge) {
	if !d.inFrame {
		// SCL activity with SDA low and no START means the capture began in
		// the middle of a transaction or after a START that was not captured.
		// Do not invent a frame; report it once.
		if !d.sdaHigh {
			i := len(d.result.Diagnostics)
			if i == 0 || d.result.Diagnostics[i-1].Code != core.CodeI2CNoStartCondition {
				d.result.Diagnostics = append(d.result.Diagnostics, newDiagnostic(
					core.CodeI2CNoStartCondition, core.SeverityWarning, timePtr(ev.time),
					"SCL edge with SDA low but no START condition; capture may begin mid-frame, so these bits are not decoded"))
			}
		}
		return
	}

	byteIndex := 0
	if d.hasAddress && !d.pendingAddr {
		byteIndex = len(d.frame.Data) + 1
	}
	direction := d.frame.Direction

	if d.expectingAck {
		ack := NACK
		if !d.sdaHigh {
			ack = ACK
		}
		d.addBitMeta(byteIndex, 8, "ack", direction, ev)
		if d.pendingAddr {
			d.frame.AddressACK = ack
			d.pendingAddr = false
		} else if d.pendingData != nil {
			d.pendingData.Ack = ack
			d.pendingData.End = ev.time
			d.frame.Data = append(d.frame.Data, *d.pendingData)
			d.pendingData = nil
		}
		d.expectingAck = false
		d.bitCount = 0
		return
	}

	bit := uint8(0)
	if d.sdaHigh {
		bit = 1
	}
	phase := "data"
	if !d.hasAddress {
		phase = "address"
	}
	d.addBitMeta(byteIndex, d.bitCount, phase, direction, ev)
	if d.bitCount == 0 {
		d.byteStart = ev.time
	}
	d.byteValue = d.byteValue<<1 | bit
	d.bitCount++

	if d.bitCount == 8 {
		if !d.hasAddress {
			d.processAddress(d.byteValue)
			d.hasAddress = true
			d.pendingAddr = true
		} else {
			d.pendingData = &Byte{
				Value: d.byteValue,
				Ack:   ACKUnknown,
				Start: d.byteStart,
				End:   ev.time,
			}
			d.pendingAddr = false
		}
		d.expectingAck = true
		d.bitCount = 0
		d.byteValue = 0
	}
}

func (d *i2cDecoder) processAddress(v uint8) {
	d.frame.AddressBits = 7
	d.frame.Address = uint16(v >> 1)
	if v&1 == 0 {
		d.frame.Direction = core.DirectionWrite
	} else {
		d.frame.Direction = core.DirectionRead
	}
	// 10-bit addressing prefix: 11110xx (first five bits 11110).
	if v&0xF8 == 0xF0 {
		d.unsupported10 = true
		d.frame.AddressBits = 0
		d.frame.Address = 0
		d.frame.Direction = core.DirectionUnknown
		d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
			core.CodeI2C10BitUnsupported, core.SeverityError, timePtr(d.frame.Start),
			"10-bit I2C addressing prefix (11110xx) is not supported; this frame is not decoded as a 7-bit address"))
	}
}

func (d *i2cDecoder) addBitMeta(byteIndex, bitIndex int, phase string, dir core.Direction, ev mergedEdge) {
	d.txMeta.bits = append(d.txMeta.bits, i2cBitMeta{
		frameIndex: len(d.txMeta.frames),
		byteIndex:  byteIndex,
		bitIndex:   bitIndex,
		phase:      phase,
		direction:  dir,
		sclRise:    ev.time,
		sclOrdinal: ev.ordinal,
	})
}

func (d *i2cDecoder) onStartOrRepeatedStart(ev mergedEdge) {
	if !d.inFrame {
		d.inFrame = true
		d.tx = &Transaction{Start: ev.time}
		d.txMeta = &i2cTxMeta{startTime: ev.time, startOrdinal: ev.ordinal}
		d.frame = &Frame{Start: ev.time, Direction: core.DirectionUnknown}
		d.fmeta = &i2cFrameMeta{startTime: ev.time, startOrdinal: ev.ordinal}
		d.hasAddress = false
		d.unsupported10 = false
		d.bitCount = 0
		d.byteValue = 0
		d.expectingAck = false
		d.pendingAddr = false
		d.pendingData = nil
		return
	}

	// Repeated START. If it happens in the middle of a byte the current frame
	// is truncated; keep what is provable and say so.
	// One trailing bit is the normal condition-setup edge and is discarded by
	// finishFrame. More than one bit, an unsampled ACK, or no address at all
	// indicates a genuine truncation.
	if d.bitCount > 1 || d.expectingAck || d.pendingData != nil || !d.hasAddress {
		d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
			core.CodeI2CStartMidByte, core.SeverityWarning, timePtr(ev.time),
			"repeated START occurred before the current byte/ACK was complete; the partial frame is truncated"))
	}
	d.finishFrame(ev.time, false, true)
	d.frame = &Frame{Start: ev.time, Direction: core.DirectionUnknown, RepeatedStart: true}
	d.fmeta = &i2cFrameMeta{startTime: ev.time, startOrdinal: ev.ordinal}
	d.hasAddress = false
	d.unsupported10 = false
	d.bitCount = 0
	d.byteValue = 0
	d.expectingAck = false
	d.pendingAddr = false
	d.pendingData = nil
}

func (d *i2cDecoder) onStop(ev mergedEdge) {
	if !d.inFrame {
		return
	}
	if d.bitCount > 1 || d.expectingAck || d.pendingData != nil || !d.hasAddress {
		d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
			core.CodeI2CStopMidByte, core.SeverityWarning, timePtr(ev.time),
			"STOP occurred before the current byte/ACK was complete; the partial frame is truncated"))
	}
	d.finishFrame(ev.time, true, true)
	d.tx.End = ev.time
	d.txMeta.stopTime = ev.time
	d.txMeta.stopOrdinal = ev.ordinal
	d.txMeta.hasStop = true
	d.tx.Complete = d.transactionComplete()
	d.finishTransaction()
	d.inFrame = false
}

func (d *i2cDecoder) finishFrame(end float64, hasStop bool, byCondition bool) {
	f := d.frame
	f.End = end
	f.HasStop = hasStop
	clean := true

	if !d.hasAddress {
		clean = false
	}
	// A STOP or repeated START is prepared by one SCL rising edge while SDA
	// holds the condition setup level. That edge is not a data bit, so a
	// single trailing bit at a condition is discarded rather than reported as
	// a truncated byte. Two or more trailing bits may be a genuinely aborted
	// byte and are diagnosed. At end of capture there is no condition to
	// explain the bit, so it is always treated as truncation.
	if d.bitCount > 0 {
		if byCondition && d.bitCount == 1 {
			if n := len(d.txMeta.bits); n > 0 {
				d.txMeta.bits = d.txMeta.bits[:n-1]
			}
		} else {
			clean = false
			d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
				core.CodeI2CTruncatedByte, core.SeverityWarning, timePtr(end),
				"byte interrupted after %d of 8 data bits; no byte value is reported for it", d.bitCount))
		}
	}
	if d.pendingAddr {
		clean = false
		d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
			core.CodeI2CMissingACK, core.SeverityWarning, timePtr(end),
			"address ACK bit was not captured; AddressACK remains unknown"))
	}
	if d.pendingData != nil {
		clean = false
		b := *d.pendingData
		f.Data = append(f.Data, b)
		d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
			core.CodeI2CMissingACK, core.SeverityWarning, timePtr(end),
			"data byte 0x%02X was captured but its ACK bit was not; Ack remains unknown", b.Value))
		d.pendingData = nil
	}
	if d.unsupported10 {
		clean = false
	}
	f.Complete = clean && f.AddressBits == 7 && f.AddressACK != ACKUnknown && !d.unsupported10
	for _, b := range f.Data {
		if b.Ack == ACKUnknown {
			f.Complete = false
			break
		}
	}
	d.tx.Frames = append(d.tx.Frames, *f)
	d.txMeta.frames = append(d.txMeta.frames, *d.fmeta)
	d.frame = nil
	d.fmeta = nil
	d.hasAddress = false
	d.bitCount = 0
	d.byteValue = 0
	d.expectingAck = false
	d.pendingAddr = false
	d.pendingData = nil
}

func (d *i2cDecoder) transactionComplete() bool {
	if len(d.tx.Frames) == 0 {
		return false
	}
	for _, f := range d.tx.Frames {
		if !f.Complete {
			return false
		}
	}
	return true
}

func (d *i2cDecoder) finishTransaction() {
	d.txMeta.bitLenEnsure()
	d.metas = append(d.metas, *d.txMeta)
	d.result.Transactions = append(d.result.Transactions, *d.tx)
	d.tx = nil
	d.txMeta = nil
}

func (d *i2cDecoder) finishAtEnd() {
	if !d.inFrame {
		return
	}
	end := d.lastActivity
	d.tx.Diagnostics = append(d.tx.Diagnostics, newDiagnostic(
		core.CodeI2CMissingStop, core.SeverityWarning, timePtr(end),
		"capture ended before a STOP condition; the transaction is incomplete"))
	d.finishFrame(end, false, false)
	d.tx.End = end
	d.txMeta.stopTime = 0
	d.txMeta.hasStop = false
	d.tx.Complete = false
	d.finishTransaction()
	d.inFrame = false
}

func (m *i2cTxMeta) bitLenEnsure() {
	if m.frames == nil {
		m.frames = []i2cFrameMeta{}
	}
	if m.bits == nil {
		m.bits = []i2cBitMeta{}
	}
}

func newDiagnostic(code, severity string, t *float64, format string, args ...any) core.Diagnostic {
	return core.NewDiagnostic(code, severity, t, format, args...)
}

func timePtr(t float64) *float64 { return &t }
