package i2c_test

import (
	"testing"

	"github.com/jkbhuan/go-decode/bus/i2c"
	"github.com/jkbhuan/go-decode/core"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

func TestDecodeDirect(t *testing.T) {
	b, err := testwave.NewI2C(testwave.I2CConfig{
		Wave:        testwave.Config{StartTime: -2e-6, SampleRate: 100e6, Low: 0, High: 3.3},
		SCLHighTime: 2e-6, SCLLowTime: 5e-6, DataHold: 1e-6, DataSetup: 4e-6,
		StartHold: 0.5e-6, StartSetup: 1.5e-6, StopSetup: 0.8e-6,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.Idle(1e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x12, true)
	b.RepeatedStart()
	b.Byte(0xA1, true)
	b.Byte(0x5A, false)
	b.Stop()
	scl, sda, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	res, err := i2c.Decode(scl, sda, i2c.Options{
		SCLThreshold: core.SingleThreshold(1.65),
		SDAThreshold: core.SingleThreshold(1.65),
		VDD:          3.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 || len(res.Frames) != 2 || len(res.Timings) == 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Frames[0].Direction != core.DirectionWrite || res.Frames[1].Direction != core.DirectionRead {
		t.Fatalf("frames = %+v", res.Frames)
	}
}
