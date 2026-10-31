package tektronix_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/core"
	"github.com/jkbhuan/go-decode/scope/tektronix"
)

func readWFMFile(path string, opts tektronix.WFMOptions) (core.Capture, error) {
	f, err := os.Open(path)
	if err != nil {
		return core.Capture{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return core.Capture{}, err
	}
	return tektronix.ReadWFM(f, st.Size(), opts)
}

func TestReadRealTektronixWFMAndDecodeI2C(t *testing.T) {
	root := filepath.Join("..", "..", "..", "生产环境测试", "i2c-tek")
	sclPath := filepath.Join(root, "scl.wfm")
	sdaPath := filepath.Join(root, "sda.wfm")
	if _, err := os.Stat(sclPath); err != nil {
		t.Skip("real Tektronix sample not present")
	}
	sclCap, err := readWFMFile(sclPath, tektronix.WFMOptions{Channel: "CH1"})
	if err != nil {
		t.Fatal(err)
	}
	sdaCap, err := readWFMFile(sdaPath, tektronix.WFMOptions{Channel: "CH2"})
	if err != nil {
		t.Fatal(err)
	}
	scl, ok := sclCap.Channel("CH1", 0)
	if !ok {
		t.Fatal("CH1 missing")
	}
	sda, ok := sdaCap.Channel("CH2", 0)
	if !ok {
		t.Fatal("CH2 missing")
	}
	if len(scl.Samples) != 3_125_000 || len(sda.Samples) != 3_125_000 {
		t.Fatalf("samples = %d/%d", len(scl.Samples), len(sda.Samples))
	}
	res, err := decode.I2C(scl, sda, decode.I2COptions{
		SCLThreshold: decode.LogicThreshold{Low: 0.99, High: 2.31},
		SDAThreshold: decode.LogicThreshold{Low: 0.99, High: 2.31},
		VDD:          3.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) == 0 || len(res.Frames) == 0 {
		t.Fatalf("no I2C decoded: %+v", res.Diagnostics)
	}
	for _, tm := range res.Timings {
		if tm.X2 < tm.X1 {
			t.Fatalf("bad timing %+v", tm)
		}
	}
}
