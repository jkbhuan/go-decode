package smi

import (
	"math"
	"sort"

	"github.com/jkbhuan/go-decode/core"
)

// MDIO timing notes (IEEE 802.3-2022, Clause 22.3.4 and Figures 22-18/22-19):
//
//   - MDC high/low/period are measured on every frame, independent of the
//     direction, using the caller-supplied timing threshold.
//   - When the STA drives MDIO (write frames), setup and hold are measured
//     relative to the MDC rising edge, as in Figure 22-18.
//   - When the PHY drives MDIO (read data after the turnaround), the clock-to-
//     output relationship is a device parameter (commonly tVAL/tOINVLD) that
//     cannot be inferred from Clause 22 alone. This implementation therefore
//     reports a structured "unsupported for this direction" diagnostic rather
//     than reusing the write-direction setup/hold definitions.
//
// As with I2C, a setup/hold value is only emitted when the MDIO transition
// that defines it was actually observed. Bits that do not change have no
// measurable transition and are not assigned a fabricated value.

type smiTimingCtx struct {
	mdc     core.LogicTrace // hysteresis: rising at Vih, falling at Vil
	mdio    core.LogicTrace
	mdcHigh core.LogicTrace // Vih crossing on both edges
	mdcLow  core.LogicTrace // Vil crossing on both edges
	tol     float64
}

func computeSMITimings(frames []Frame, metas []smiFrameMeta, mdcT, mdioT, mdcHighT, mdcLowT core.LogicTrace) {
	ctx := &smiTimingCtx{
		mdc:     mdcT,
		mdio:    mdioT,
		mdcHigh: mdcHighT,
		mdcLow:  mdcLowT,
		tol:     core.EdgeMatchTolerance(mdcT.Edges),
	}
	for i := range frames {
		f := &frames[i]
		if i >= len(metas) {
			continue
		}
		meta := &metas[i]
		ctx.addClockTimings(i, f)
		if f.Direction == core.DirectionRead {
			f.Diagnostics = append(f.Diagnostics, newDiagnostic(
				core.CodeSMIReadTimingUnsupported, core.SeverityInfo, timePtr(f.End),
				"read-direction MDIO setup/hold was not measured: after the turnaround the PHY drives MDIO, and Clause 22 does not define the same tSU/tHD parameters for PHY-sourced data (see device tVAL/tOINVLD limits)"))
			continue
		}
		if f.Direction != core.DirectionWrite || !f.Complete {
			continue
		}
		ctx.addWriteSetupHold(i, f, meta)
	}
}

func (c *smiTimingCtx) addClockTimings(frameIndex int, f *Frame) {
	highCount, lowCount, periodCount := 0, 0, 0
	// HIGH: above Vih(min) on both edges (IEEE 802.3 22.3).
	lo := sort.Search(len(c.mdcHigh.Edges), func(i int) bool { return c.mdcHigh.Edges[i].Time >= f.Start })
	hi := sort.Search(len(c.mdcHigh.Edges), func(i int) bool { return c.mdcHigh.Edges[i].Time > f.End })
	for i := lo; i+1 < hi; i++ {
		a, b := c.mdcHigh.Edges[i], c.mdcHigh.Edges[i+1]
		if a.Kind == core.EdgeRising && b.Kind == core.EdgeFalling {
			appendTiming(&f.Timings, core.TimingMDCHigh, a.Time, b.Time, frameIndex, -1, -1, "clock", core.DirectionUnknown)
			highCount++
		}
	}
	// LOW: below Vil(max) on both edges.
	lo = sort.Search(len(c.mdcLow.Edges), func(i int) bool { return c.mdcLow.Edges[i].Time >= f.Start })
	hi = sort.Search(len(c.mdcLow.Edges), func(i int) bool { return c.mdcLow.Edges[i].Time > f.End })
	for i := lo; i+1 < hi; i++ {
		a, b := c.mdcLow.Edges[i], c.mdcLow.Edges[i+1]
		if a.Kind == core.EdgeFalling && b.Kind == core.EdgeRising {
			appendTiming(&f.Timings, core.TimingMDCLow, a.Time, b.Time, frameIndex, -1, -1, "clock", core.DirectionUnknown)
			lowCount++
		}
	}
	// Period: rising edge to the next rising edge at Vih.
	lo = sort.Search(len(c.mdcHigh.Edges), func(i int) bool { return c.mdcHigh.Edges[i].Time >= f.Start })
	hi = sort.Search(len(c.mdcHigh.Edges), func(i int) bool { return c.mdcHigh.Edges[i].Time > f.End })
	for i := lo; i+2 < hi; i++ {
		a, mid, b := c.mdcHigh.Edges[i], c.mdcHigh.Edges[i+1], c.mdcHigh.Edges[i+2]
		if a.Kind == core.EdgeRising && mid.Kind == core.EdgeFalling && b.Kind == core.EdgeRising {
			appendTiming(&f.Timings, core.TimingMDCPeriod, a.Time, b.Time, frameIndex, -1, -1, "clock", core.DirectionUnknown)
			periodCount++
		}
	}
	if highCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"MDCHigh: no complete Vih-to-Vih high period was observed inside the frame"))
	}
	if lowCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"MDCLow: no complete Vil-to-Vil low period was observed inside the frame"))
	}
	if periodCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"MDCPeriod: fewer than two MDC rising edges were observed inside the frame"))
	}
}

func (c *smiTimingCtx) addWriteSetupHold(frameIndex int, f *Frame, meta *smiFrameMeta) {
	if len(meta.bits) == 0 {
		return
	}
	suCount, hdCount := 0, 0
	for _, b := range meta.bits {
		rise, idx, ok := c.matchMDCRise(b.mdcOrdinal, b.mdcRise)
		if !ok || idx < 1 {
			continue
		}
		prev := c.mdc.Edges[idx-1]
		if prev.Kind == core.EdgeFalling {
			if e, ok := core.LastEdgeBetween(c.mdio.Edges, prev.Time, rise.Time); ok {
				appendTiming(&f.Timings, core.TimingSUMDIO, e.Time, rise.Time, frameIndex, -1, b.bitIndex, b.phase, core.DirectionWrite)
				suCount++
			}
		}
		nextRise, ok := core.FirstEdgeAfter(c.mdc.Edges, rise.Time, core.EdgeRising)
		if ok && nextRise.Time <= f.End+c.tol {
			if e, ok := core.FirstEdgeBetween(c.mdio.Edges, rise.Time, nextRise.Time); ok {
				appendTiming(&f.Timings, core.TimingHDMDIO, rise.Time, e.Time, frameIndex, -1, b.bitIndex, b.phase, core.DirectionWrite)
				hdCount++
			}
		}
	}
	if suCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"tSU(MDIO): no MDIO transition was directly observable before an MDC rising edge in this write frame"))
	}
	if hdCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"tHD(MDIO): no MDIO transition was directly observable after an MDC rising edge in this write frame"))
	}
}

func (c *smiTimingCtx) matchMDCRise(ordinal int, logicTime float64) (core.Edge, int, bool) {
	if ordinal >= 0 && ordinal < len(c.mdc.Edges) {
		e := c.mdc.Edges[ordinal]
		if e.Kind == core.EdgeRising && math.Abs(e.Time-logicTime) <= c.tol {
			return e, ordinal, true
		}
	}
	best, bestDiff := -1, math.Inf(1)
	for i, e := range c.mdc.Edges {
		if e.Kind != core.EdgeRising {
			continue
		}
		d := math.Abs(e.Time - logicTime)
		if d <= c.tol && d < bestDiff {
			best, bestDiff = i, d
		}
	}
	if best < 0 {
		return core.Edge{}, -1, false
	}
	return c.mdc.Edges[best], best, true
}

func appendTiming(dst *[]core.Timing, name core.TimingName, x1, x2 float64, frame, byteIdx, bit int, phase string, dir core.Direction) {
	if x2 < x1 {
		return
	}
	*dst = append(*dst, core.NewTiming(name, x1, x2, frame, byteIdx, bit, phase, dir))
}
