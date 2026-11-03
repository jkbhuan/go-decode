package core_test

import (
	"testing"

	"github.com/jkbhuan/go-decode/core"
)

func TestCaptureChannelAndValidation(t *testing.T) {
	w := core.Waveform{Samples: []float32{0, 1}, SampleRate: 1e6, StartTime: -1e-6}
	c := core.Capture{Records: []core.Record{
		{Channel: "CH1", SegmentIndex: 0, Waveform: w},
		{Channel: "CH1", SegmentIndex: 1, Waveform: w},
	}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Channel("CH1", 1); !ok || len(got.Samples) != 2 {
		t.Fatal("channel lookup failed")
	}
	if got := c.Channels(); len(got) != 1 || got[0] != "CH1" {
		t.Fatalf("channels = %v", got)
	}
	if got := c.Segments(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("segments = %v", got)
	}
	dup := core.Capture{Records: []core.Record{
		{Channel: "CH1", Waveform: w},
		{Channel: "CH1", Waveform: w},
	}}
	if err := dup.Validate(); err == nil {
		t.Fatal("expected duplicate-record error")
	}
}
