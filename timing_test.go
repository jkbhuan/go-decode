package decode_test

import (
	"testing"

	"github.com/jkbhuan/go-decode"
)

func sampleTimings() []decode.Timing {
	return []decode.Timing{
		{Name: decode.TimingSUDAT, Value: 3, X1: 10, X2: 13, FrameIndex: 0, ByteIndex: 1, BitIndex: 2, Phase: "data", Direction: decode.DirectionWrite},
		{Name: decode.TimingSUDAT, Value: 1, X1: 20, X2: 21, FrameIndex: 0, ByteIndex: 1, BitIndex: 3, Phase: "data", Direction: decode.DirectionWrite},
		{Name: decode.TimingSUDAT, Value: 2, X1: 30, X2: 32, FrameIndex: 1, ByteIndex: 2, BitIndex: 0, Phase: "data", Direction: decode.DirectionRead},
		{Name: decode.TimingHDSTA, Value: 5, X1: 0, X2: 5, FrameIndex: 0, ByteIndex: -1, BitIndex: -1, Phase: "start", Direction: decode.DirectionWrite},
	}
}

func TestSelectTimingModes(t *testing.T) {
	all := sampleTimings()
	filter := decode.TimingFilter{Name: decode.TimingSUDAT}

	got, ok := decode.SelectTiming(all, filter, decode.SelectFirst)
	if !ok || got.X1 != 10 {
		t.Fatalf("First: %+v, ok=%v", got, ok)
	}
	got, ok = decode.SelectTiming(all, filter, decode.SelectMin)
	if !ok || got.Value != 1 || got.X1 != 20 {
		t.Fatalf("Min: %+v, ok=%v", got, ok)
	}
	got, ok = decode.SelectTiming(all, filter, decode.SelectMax)
	if !ok || got.Value != 3 || got.X1 != 10 {
		t.Fatalf("Max: %+v, ok=%v", got, ok)
	}
}

func TestSelectTimingFilters(t *testing.T) {
	all := sampleTimings()
	direction := decode.DirectionRead
	filter := decode.TimingFilter{Name: decode.TimingSUDAT, Direction: direction}
	got, ok := decode.SelectTiming(all, filter, decode.SelectFirst)
	if !ok || got.FrameIndex != 1 {
		t.Fatalf("direction filter: %+v, ok=%v", got, ok)
	}

	frame := 0
	filter = decode.TimingFilter{Name: decode.TimingSUDAT, FrameIndex: &frame}
	if got, ok = decode.SelectTiming(all, filter, decode.SelectMax); !ok || got.Value != 3 {
		t.Fatalf("frame filter: %+v, ok=%v", got, ok)
	}

	byteIdx := 2
	filter = decode.TimingFilter{ByteIndex: &byteIdx}
	if got, ok = decode.SelectTiming(all, filter, decode.SelectFirst); !ok || got.FrameIndex != 1 {
		t.Fatalf("byte filter: %+v, ok=%v", got, ok)
	}

	filter = decode.TimingFilter{Phase: "start"}
	if got, ok = decode.SelectTiming(all, filter, decode.SelectFirst); !ok || got.Name != decode.TimingHDSTA {
		t.Fatalf("phase filter: %+v, ok=%v", got, ok)
	}
}

func TestSelectTimingTieBreaksByEarliestX1(t *testing.T) {
	all := []decode.Timing{
		{Name: "x", Value: 2, X1: 20, X2: 22},
		{Name: "x", Value: 2, X1: 10, X2: 12},
		{Name: "x", Value: 2, X1: 30, X2: 32},
	}
	for _, mode := range []decode.SelectionMode{decode.SelectFirst, decode.SelectMin, decode.SelectMax} {
		got, ok := decode.SelectTiming(all, decode.TimingFilter{Name: "x"}, mode)
		if !ok || got.X1 != 10 {
			t.Fatalf("mode %d: %+v, ok=%v", mode, got, ok)
		}
	}
}

func TestSelectTimingNoCandidates(t *testing.T) {
	if _, ok := decode.SelectTiming(sampleTimings(), decode.TimingFilter{Name: "missing"}, decode.SelectFirst); ok {
		t.Fatal("expected no candidate")
	}
}

func TestSelectTimingIsDeterministic(t *testing.T) {
	all := sampleTimings()
	a, _ := decode.SelectTiming(all, decode.TimingFilter{Name: decode.TimingSUDAT}, decode.SelectMin)
	b, _ := decode.SelectTiming(all, decode.TimingFilter{Name: decode.TimingSUDAT}, decode.SelectMin)
	if a != b {
		t.Fatalf("unstable selection: %+v vs %+v", a, b)
	}
}

func TestSelectTimingsReturnsAllInOrder(t *testing.T) {
	all := sampleTimings()
	got := decode.SelectTimings(all, decode.TimingFilter{Name: decode.TimingSUDAT, Direction: decode.DirectionWrite})
	if len(got) != 2 || got[0].X1 != 10 || got[1].X1 != 20 {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}
