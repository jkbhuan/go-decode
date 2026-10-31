package smi_test

import (
	"testing"

	"github.com/jkbhuan/go-decode/bus/smi"
	"github.com/jkbhuan/go-decode/core"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

func TestDecodeDirect(t *testing.T) {
	b, err := testwave.NewMDIO(testwave.MDIOConfig{
		Wave:      testwave.Config{StartTime: 0, SampleRate: 100e6, Low: 0, High: 3.3},
		MDCPeriod: 1e-6, MDCHighTime: 0.5e-6, DataHold: 0.2e-6, DataSetup: 0.3e-6,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.Preamble(32)
	b.Frame(core.DirectionWrite, 0x05, 0x1F, 0xBEEF)
	mdc, mdio, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	res, err := smi.Decode(mdc, mdio, smi.Options{
		MDCThreshold:    core.SingleThreshold(1.65),
		MDIOThreshold:   core.SingleThreshold(1.65),
		TimingThreshold: &core.LogicThreshold{Low: 0.8, High: 2.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 || len(res.Timings) == 0 {
		t.Fatalf("result = %+v", res)
	}
	f := res.Frames[0]
	if f.Direction != core.DirectionWrite || f.PHYAddr != 5 || f.RegAddr != 0x1F || f.Data != 0xBEEF {
		t.Fatalf("frame = %+v", f)
	}
}
