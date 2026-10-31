package core

import (
	"math"
	"sort"
)

// EdgeMatchTolerance returns a time tolerance for matching an edge between
// two threshold traces of the same channel.
func EdgeMatchTolerance(edges []Edge) float64 {
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
	tol := 0.5 * gaps[len(gaps)/2]
	if tol <= 0 {
		tol = 1e-6
	}
	return tol
}

// FirstEdgeAfter returns the first edge of the requested kind strictly after t.
func FirstEdgeAfter(edges []Edge, t float64, kind EdgeKind) (Edge, bool) {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t })
	for ; i < len(edges); i++ {
		if edges[i].Kind == kind {
			return edges[i], true
		}
	}
	return Edge{}, false
}

// LastEdgeIndexAtOrBefore returns the index of the last edge at or before t.
func LastEdgeIndexAtOrBefore(edges []Edge, t float64) int {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t })
	return i - 1
}

// LastEdgeBetween returns the last edge in (t0, t1].
func LastEdgeBetween(edges []Edge, t0, t1 float64) (Edge, bool) {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t0 })
	var found Edge
	ok := false
	for ; i < len(edges) && edges[i].Time <= t1; i++ {
		found = edges[i]
		ok = true
	}
	return found, ok
}

// FirstEdgeBetween returns the first edge strictly between t0 and t1.
func FirstEdgeBetween(edges []Edge, t0, t1 float64) (Edge, bool) {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].Time > t0 })
	if i < len(edges) && edges[i].Time < t1 {
		return edges[i], true
	}
	return Edge{}, false
}

// NewTiming builds one measured timing candidate. It does not assign a
// TransactionIndex; the owning decoder sets that when it flattens results.
func NewTiming(name TimingName, x1, x2 float64, frame, byteIdx, bit int, phase string, dir Direction) Timing {
	return Timing{
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

// MatchEdge finds the edge corresponding to a logic-threshold edge in a timing
// threshold trace. It first tries the recorded ordinal, then a nearest same-kind
// edge within tol.
func MatchEdge(edges []Edge, ordinal int, kind EdgeKind, logicTime, tol float64) (Edge, int, bool) {
	if ordinal >= 0 && ordinal < len(edges) {
		e := edges[ordinal]
		if e.Kind == kind && math.Abs(e.Time-logicTime) <= tol {
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
		if diff <= tol && diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	if best < 0 {
		return Edge{}, -1, false
	}
	return edges[best], best, true
}

// HasTiming reports whether a candidate of the given name exists.
func HasTiming(ts []Timing, name TimingName) bool {
	for _, t := range ts {
		if t.Name == name {
			return true
		}
	}
	return false
}
