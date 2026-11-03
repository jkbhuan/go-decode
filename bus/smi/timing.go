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
//   - When the PHY drives MDIO (read frames, from the second turnaround bit
//     onward), tSU/tHD are measured around the MDC sampling edge and reported
//     with DirectionRead. The hold measurement is the PHY clock-to-output
//     delay defined by Figure 22-19 (0 ns to 300 ns). The names stay the same
//     as in the write direction; Direction distinguishes the two.
//
// As with I2C, a timing value is only emitted when the MDIO transition that
// defines it was actually observed. Bits that do not change have no measurable
// transition and are not assigned a fabricated value.

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
			ctx.addReadSetupHold(i, f, meta)
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

// addReadSetupHold measures tSU(MDIO)/tHD(MDIO) for the PHY-driven part of a
// read frame, using the same names as the write direction and DirectionRead to
// distinguish them.
//
// The PHY starts driving MDIO at the second turnaround bit (bitIndex 15); ST,
// OP, PHYAD, REGAD and the first turnaround bit are still driven by the STA or
// released to the pull-up. A bit is sampled on its own MDC rising edge, so the
// PHY launches that bit after the preceding rising edge (IEEE 802.3 22.3.4,
// Figure 22-19):
//
//   - tSU(MDIO) is the preceding rising edge to the launch transition, i.e.
//     the time the PHY output is stable before the sampling edge.
//   - tHD(MDIO) is the sampling edge to the next launch transition, i.e. the
//     PHY clock-to-output delay after the edge.
//
// A value is emitted only when the corresponding transition is actually
// visible. Bits that do not change have no output transition and therefore no
// fabricated value.
func (c *smiTimingCtx) addReadSetupHold(frameIndex int, f *Frame, meta *smiFrameMeta) {
	suCount, hdCount := 0, 0
	for _, b := range meta.bits {
		if b.bitIndex < readFirstPHYDrivenBit {
			continue
		}
		rise, idx, ok := c.matchMDCRise(b.mdcOrdinal, b.mdcRise)
		if !ok {
			continue
		}
		prevRise, ok := previousEdgeOfKind(c.mdc.Edges, idx, core.EdgeRising)
		if ok && prevRise.Time >= f.Start-c.tol {
			if e, ok := core.LastEdgeBetween(c.mdio.Edges, prevRise.Time, rise.Time); ok {
				appendTiming(&f.Timings, core.TimingSUMDIO, e.Time, rise.Time, frameIndex, -1, b.bitIndex, b.phase, core.DirectionRead)
				suCount++
			}
		}
		nextRise, ok := core.FirstEdgeAfter(c.mdc.Edges, rise.Time, core.EdgeRising)
		if ok && nextRise.Time <= f.End+c.tol {
			if e, ok := core.FirstEdgeBetween(c.mdio.Edges, rise.Time, nextRise.Time); ok {
				appendTiming(&f.Timings, core.TimingHDMDIO, rise.Time, e.Time, frameIndex, -1, b.bitIndex, b.phase, core.DirectionRead)
				hdCount++
			}
		}
	}
	if suCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"tSU(MDIO): no PHY-driven MDIO transition was observable before an MDC rising edge in this read frame"))
	}
	if hdCount == 0 {
		f.Diagnostics = append(f.Diagnostics, newDiagnostic(
			core.CodeTimingNotObservable, core.SeverityInfo, timePtr(f.End),
			"tHD(MDIO): no PHY clock-to-output transition was observable after an MDC rising edge in this read frame"))
	}
}

// readFirstPHYDrivenBit is the first bit of the Clause 22 frame that the PHY
// drives in a read transaction: the second turnaround bit.
const readFirstPHYDrivenBit = 15

// previousEdgeOfKind returns the closest edge of the requested kind strictly
// before index idx.
func previousEdgeOfKind(edges []core.Edge, idx int, kind core.EdgeKind) (core.Edge, bool) {
	for i := idx - 1; i >= 0; i-- {
		if edges[i].Kind == kind {
			return edges[i], true
		}
	}
	return core.Edge{}, false
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
