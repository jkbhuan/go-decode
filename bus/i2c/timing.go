package i2c

import (
	"fmt"
	"math"
	"sort"

	"github.com/jkbhuan/go-decode/core"
)

// I2C timing reference levels:
//
// The timing traces use a hysteresis threshold with Low = 0.3*VDD (V_IL) and
// High = 0.7*VDD (V_IH), as in NXP UM10204 Rev. 7 Figure 38. Consequently a
// falling edge is reported where the signal crosses 0.3*VDD and a rising edge
// where it crosses 0.7*VDD. These points are the ones used for:
//
//	tHD;STA  SDA fall (0.3*VDD)      -> next SCL fall (0.3*VDD)
//	tSU;STA  SCL rise (0.7*VDD)      -> repeated START SDA fall (0.3*VDD)
//	tSU;STO  SCL rise (0.7*VDD)      -> STOP SDA rise (0.7*VDD)
//	tBUF     STOP SDA rise (0.7*VDD) -> next START SDA fall (0.3*VDD)
//	tSU;DAT  SDA transition          -> SCL rise (0.7*VDD)
//	tHD;DAT  SCL fall (0.3*VDD)      -> next SDA transition
//	tHIGH    SCL rise (0.7*VDD)      -> SCL fall (0.3*VDD)
//	tLOW     SCL fall (0.3*VDD)      -> SCL rise (0.7*VDD)
//
// A setup/hold value is only produced when the SDA transition that defines it
// is itself visible in the record. Bits that do not change state have no
// observable transition and therefore produce no fabricated value.

type i2cTimingCtx struct {
	scl core.LogicTrace
	sda core.LogicTrace
	tol float64
}

func computeI2CTimings(txs []Transaction, metas []i2cTxMeta, sclT, sdaT core.LogicTrace) {
	ctx := &i2cTimingCtx{
		scl: sclT,
		sda: sdaT,
		tol: edgeMatchTolerance(sclT.Edges),
	}

	for i := range txs {
		tx := &txs[i]
		if i >= len(metas) {
			continue
		}
		meta := &metas[i]
		ctx.computeTransaction(i, tx, meta, metas)
		ctx.addMissingDiagnostics(i, tx, metas)
	}
}

func edgeMatchTolerance(edges []core.Edge) float64 {
	if len(edges) < 2 {
		return 1e-6
	}
	gaps := make([]float64, 0, len(edges)-1)
	for i := 1; i < len(edges); i++ {
		d := edges[i].Time - edges[i-1].Time
		if d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return 1e-6
	}
	sort.Float64s(gaps)
	med := gaps[len(gaps)/2]
	tol := 0.5 * med
	if tol <= 0 {
		tol = 1e-6
	}
	return tol
}

func (c *i2cTimingCtx) matchEdge(edges []core.Edge, ordinal int, kind core.EdgeKind, logicTime float64) (core.Edge, int, bool) {
	if ordinal >= 0 && ordinal < len(edges) {
		e := edges[ordinal]
		if e.Kind == kind && math.Abs(e.Time-logicTime) <= c.tol {
			return e, ordinal, true
		}
	}
	best := -1
	bestDiff := math.Inf(1)
	for i, e := range edges {
		if e.Kind != kind {
			continue
		}
		diff := math.Abs(e.Time - logicTime)
		if diff <= c.tol && diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	if best < 0 {
		return core.Edge{}, -1, false
	}
	return edges[best], best, true
}

func (c *i2cTimingCtx) matchSCL(ordinal int, kind core.EdgeKind, logicTime float64) (core.Edge, int, bool) {
	return c.matchEdge(c.scl.Edges, ordinal, kind, logicTime)
}

func (c *i2cTimingCtx) matchSDA(ordinal int, kind core.EdgeKind, logicTime float64) (core.Edge, int, bool) {
	return c.matchEdge(c.sda.Edges, ordinal, kind, logicTime)
}

func (c *i2cTimingCtx) computeTransaction(idx int, tx *Transaction, meta *i2cTxMeta, metas []i2cTxMeta) {
	v := &timingValidator{tx: tx}

	// Resolve frame start/stop times in the timing threshold domain.
	frameStarts := make([]float64, len(meta.frames))
	frameStartsOK := make([]bool, len(meta.frames))
	for f := range meta.frames {
		if e, _, ok := c.matchSDA(meta.frames[f].startOrdinal, core.EdgeFalling, meta.frames[f].startTime); ok {
			frameStarts[f] = e.Time
			frameStartsOK[f] = true
		}
	}

	var stopTime float64
	var stopOK bool
	if meta.hasStop {
		if e, _, ok := c.matchSDA(meta.stopOrdinal, core.EdgeRising, meta.stopTime); ok {
			stopTime, stopOK = e.Time, true
		}
	}

	for f := range tx.Frames {
		if f >= len(meta.frames) || f >= len(frameStartsOK) {
			break
		}
		frame := &tx.Frames[f]
		if !frameStartsOK[f] {
			continue
		}
		startTime := frameStarts[f]

		// tHD;STA: START/repeated START to the following SCL falling edge,
		// bounded by the end of this frame.
		endBound := c.scl.EndTime
		if f+1 < len(meta.frames) && frameStartsOK[f+1] {
			endBound = frameStarts[f+1]
		} else if stopOK {
			endBound = stopTime
		}
		if fall, ok := firstEdgeAfter(c.scl.Edges, startTime, core.EdgeFalling); ok && fall.Time < endBound {
			v.add(core.TimingHDSTA, startTime, fall.Time, f, -1, -1, "start", frame.Direction)
		}

		// tSU;STA applies only to a repeated START. An initial START after an
		// idle bus has no preceding SCL high period, so no value is invented.
		if frame.RepeatedStart {
			if prevIdx := lastEdgeIndexAtOrBefore(c.scl.Edges, startTime); prevIdx >= 0 {
				prev := c.scl.Edges[prevIdx]
				if prev.Kind == core.EdgeRising {
					v.add(core.TimingSUSTA, prev.Time, startTime, f, -1, -1, "start", frame.Direction)
				}
			}
		}
	}

	// tSU;STO: last SCL rising edge before the STOP to the STOP's SDA rise.
	if stopOK {
		if idx := lastEdgeIndexAtOrBefore(c.scl.Edges, stopTime); idx >= 0 {
			prev := c.scl.Edges[idx]
			if prev.Kind == core.EdgeRising {
				frameIdx, dir := -1, core.DirectionUnknown
				if n := len(tx.Frames); n > 0 {
					frameIdx = n - 1
					dir = tx.Frames[n-1].Direction
				}
				v.add(core.TimingSUSTO, prev.Time, stopTime, frameIdx, -1, -1, "stop", dir)
			}
		}
	}

	// tBUF: attached to the transaction that follows the STOP. The previous
	// STOP and the current START are both required.
	if idx > 0 {
		prev := &metas[idx-1]
		if prev.hasStop {
			if pe, _, ok := c.matchSDA(prev.stopOrdinal, core.EdgeRising, prev.stopTime); ok {
				if ce, _, ok := c.matchSDA(meta.startOrdinal, core.EdgeFalling, meta.startTime); ok {
					v.add(core.TimingBUF, pe.Time, ce.Time, 0, -1, -1, "busfree", core.DirectionUnknown)
				}
			}
		}
	}

	// Per-bit setup and hold. Only directly observed SDA transitions produce
	// a value; a bit that keeps the previous state has nothing to measure.
	for _, b := range meta.bits {
		rise, riseIdx, ok := c.matchSCL(b.sclOrdinal, core.EdgeRising, b.sclRise)
		if !ok || riseIdx < 1 || riseIdx+1 >= len(c.scl.Edges) {
			continue
		}
		// Address bits are sampled before the R/W bit is known, so their
		// metadata starts as unknown. Once the frame is decoded, use the
		// frame direction for those candidates.
		dir := b.direction
		if dir == core.DirectionUnknown && b.frameIndex >= 0 && b.frameIndex < len(tx.Frames) {
			dir = tx.Frames[b.frameIndex].Direction
		}
		prev := c.scl.Edges[riseIdx-1]
		next := c.scl.Edges[riseIdx+1]
		if prev.Kind == core.EdgeFalling {
			if sdaEdge, ok := lastEdgeBetween(c.sda.Edges, prev.Time, rise.Time); ok {
				v.add(core.TimingSUDAT, sdaEdge.Time, rise.Time, b.frameIndex, b.byteIndex, b.bitIndex, b.phase, dir)
			}
		}
		if next.Kind == core.EdgeFalling && riseIdx+2 < len(c.scl.Edges) && c.scl.Edges[riseIdx+2].Kind == core.EdgeRising {
			nextRise := c.scl.Edges[riseIdx+2]
			if sdaEdge, ok := firstEdgeBetween(c.sda.Edges, next.Time, nextRise.Time); ok {
				v.add(core.TimingHDDAT, next.Time, sdaEdge.Time, b.frameIndex, b.byteIndex, b.bitIndex, b.phase, dir)
			}
		}
	}

	// Clock high/low/frequency. These are transaction-level quantities and
	// intentionally carry no frame/byte/bit ownership.
	clockStart := tx.Start
	if len(frameStartsOK) > 0 && frameStartsOK[0] {
		clockStart = frameStarts[0]
	}
	clockEnd := tx.End
	if stopOK {
		clockEnd = stopTime
	}
	c.addClockTimings(tx, clockStart, clockEnd)
}

func (c *i2cTimingCtx) addClockTimings(tx *Transaction, start, end float64) {
	lo := sort.Search(len(c.scl.Edges), func(i int) bool { return c.scl.Edges[i].Time >= start })
	hi := sort.Search(len(c.scl.Edges), func(i int) bool { return c.scl.Edges[i].Time > end })
	for i := lo; i+1 < hi; i++ {
		a, b := c.scl.Edges[i], c.scl.Edges[i+1]
		switch {
		case a.Kind == core.EdgeRising && b.Kind == core.EdgeFalling:
			tx.Timings = append(tx.Timings, newTiming(core.TimingTHIGH, a.Time, b.Time, -1, -1, -1, "clock", core.DirectionUnknown))
		case a.Kind == core.EdgeFalling && b.Kind == core.EdgeRising:
			tx.Timings = append(tx.Timings, newTiming(core.TimingTLOW, a.Time, b.Time, -1, -1, -1, "clock", core.DirectionUnknown))
		}
	}
	for i := lo; i+2 < hi; i++ {
		a, mid, b := c.scl.Edges[i], c.scl.Edges[i+1], c.scl.Edges[i+2]
		if a.Kind == core.EdgeRising && mid.Kind == core.EdgeFalling && b.Kind == core.EdgeRising && b.Time > a.Time {
			tx.SCLFrequencies = append(tx.SCLFrequencies, core.FrequencyMeasurement{
				Value: 1 / (b.Time - a.Time),
				X1:    a.Time,
				X2:    b.Time,
			})
		}
	}
}

func newTiming(name core.TimingName, x1, x2 float64, frame, byteIdx, bit int, phase string, dir core.Direction) core.Timing {
	return core.Timing{
		Name:       name,
		Value:      x2 - x1,
		X1:         x1,
		X2:         x2,
		FrameIndex: frame,
		ByteIndex:  byteIdx,
		BitIndex:   bit,
		Phase:      phase,
		Direction:  dir,
	}
}

type timingValidator struct {
	tx *Transaction
}

func (v *timingValidator) add(name core.TimingName, x1, x2 float64, frame, byteIdx, bit int, phase string, dir core.Direction) {
	if x2 < x1 {
		// Different threshold crossings should never invert an interval. If
		// they do, the event pairing is unreliable: omit rather than invent.
		return
	}
	v.tx.Timings = append(v.tx.Timings, newTiming(name, x1, x2, frame, byteIdx, bit, phase, dir))
}

func firstEdgeAfter(edges []core.Edge, t float64, kind core.EdgeKind) (core.Edge, bool) {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t })
	for ; i < len(edges); i++ {
		if edges[i].Kind == kind {
			return edges[i], true
		}
	}
	return core.Edge{}, false
}

func lastEdgeIndexAtOrBefore(edges []core.Edge, t float64) int {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t })
	return i - 1
}

func lastEdgeBetween(edges []core.Edge, t0, t1 float64) (core.Edge, bool) {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t0 })
	var found core.Edge
	ok := false
	for ; i < len(edges) && edges[i].Time <= t1; i++ {
		found = edges[i]
		ok = true
	}
	return found, ok
}

func firstEdgeBetween(edges []core.Edge, t0, t1 float64) (core.Edge, bool) {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t0 })
	if i < len(edges) && edges[i].Time < t1 {
		return edges[i], true
	}
	return core.Edge{}, false
}

func hasTiming(ts []core.Timing, name core.TimingName) bool {
	for _, t := range ts {
		if t.Name == name {
			return true
		}
	}
	return false
}

func (c *i2cTimingCtx) addMissingDiagnostics(idx int, tx *Transaction, metas []i2cTxMeta) {
	notObservable := func(name core.TimingName, severity, format string, args ...any) {
		tx.Diagnostics = append(tx.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, severity, timePtr(tx.End),
			fmt.Sprintf("%s: ", name)+format, args...))
	}

	if !hasTiming(tx.Timings, core.TimingHDSTA) {
		notObservable(core.TimingHDSTA, core.SeverityWarning, "no SCL falling edge was observed after the START/repeated START within the frame")
	}
	if !hasTiming(tx.Timings, core.TimingSUSTA) {
		hasRepeated := false
		for _, f := range tx.Frames {
			if f.RepeatedStart {
				hasRepeated = true
				break
			}
		}
		if !hasRepeated {
			notObservable(core.TimingSUSTA, core.SeverityInfo, "no repeated START in this transaction; the specification defines this parameter for repeated STARTs (an initial START is not given a fabricated value)")
		} else {
			notObservable(core.TimingSUSTA, core.SeverityWarning, "the SCL edge preceding the repeated START is not a rising edge, or no such edge was captured")
		}
	}
	if !hasTiming(tx.Timings, core.TimingSUSTO) {
		hasStop := false
		for _, f := range tx.Frames {
			if f.HasStop {
				hasStop = true
				break
			}
		}
		if !hasStop {
			notObservable(core.TimingSUSTO, core.SeverityInfo, "transaction has no STOP condition in the record")
		} else {
			notObservable(core.TimingSUSTO, core.SeverityWarning, "no SCL rising edge was observed before the STOP")
		}
	}
	if !hasTiming(tx.Timings, core.TimingBUF) {
		switch {
		case idx == 0:
			notObservable(core.TimingBUF, core.SeverityInfo, "no preceding STOP condition in the record (this is the first transaction)")
		case idx-1 < len(metas) && !metas[idx-1].hasStop:
			notObservable(core.TimingBUF, core.SeverityInfo, "the previous transaction has no STOP condition in the record")
		default:
			notObservable(core.TimingBUF, core.SeverityWarning, "the previous STOP or this START could not be matched on the timing threshold traces")
		}
	}
	if !hasTiming(tx.Timings, core.TimingSUDAT) {
		if len(tx.Frames) == 0 {
			notObservable(core.TimingSUDAT, core.SeverityInfo, "no decoded bits")
		} else {
			notObservable(core.TimingSUDAT, core.SeverityWarning, "no directly observable SDA transition between the preceding SCL falling edge and any sampling edge")
		}
	}
	if !hasTiming(tx.Timings, core.TimingHDDAT) {
		notObservable(core.TimingHDDAT, core.SeverityWarning, "no directly observable SDA transition after an SCL falling edge and before the next sampling edge")
	}
	if !hasTiming(tx.Timings, core.TimingTHIGH) {
		notObservable(core.TimingTHIGH, core.SeverityInfo, "no complete SCL high period inside the transaction")
	}
	if !hasTiming(tx.Timings, core.TimingTLOW) {
		notObservable(core.TimingTLOW, core.SeverityInfo, "no complete SCL low period inside the transaction")
	}
	if len(tx.SCLFrequencies) == 0 {
		notObservable("fSCL", core.SeverityInfo, "no complete SCL cycle (rise-to-rise) inside the transaction")
	}
}
