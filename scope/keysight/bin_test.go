package keysight_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/core"
	"github.com/jkbhuan/go-decode/scope/keysight"
)

func readBinFile(path string, opts keysight.BinOptions) (core.Capture, error) {
	f, err := os.Open(path)
	if err != nil {
		return core.Capture{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return core.Capture{}, err
	}
	return keysight.ReadBin(f, st.Size(), opts)
}

func TestReadRealKeysightBinAndDecodeI2C(t *testing.T) {
	root := filepath.Join("..", "..", "..", "生产环境测试", "i2c-keysight")
	sclPath := filepath.Join(root, "scl.bin")
	sdaPath := filepath.Join(root, "sda.bin")
	if _, err := os.Stat(sclPath); err != nil {
		t.Skip("real Keysight sample not present")
	}
	sclCap, err := readBinFile(sclPath, keysight.BinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sdaCap, err := readBinFile(sdaPath, keysight.BinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sclCap.Source.Model != "MXR604A" {
		t.Fatalf("model = %q", sclCap.Source.Model)
	}
	scl, ok := sclCap.Channel("Channel 1", 0)
	if !ok {
		t.Fatalf("CH1 missing; channels=%v", sclCap.Channels())
	}
	sda, ok := sdaCap.Channel("Channel 2", 0)
	if !ok {
		t.Fatalf("CH2 missing; channels=%v", sdaCap.Channels())
	}
	res, err := decode.I2C(scl, sda, decode.I2COptions{
		SCLThreshold: decode.LogicThreshold{Low: 0.99, High: 2.31},
		SDAThreshold: decode.LogicThreshold{Low: 0.99, High: 2.31},
		VDD:          3.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 2 || len(res.Frames) != 3 {
		t.Fatalf("transactions=%d frames=%d", len(res.Transactions), len(res.Frames))
	}
	want := []struct {
		addr uint16
		dir  decode.Direction
		data uint8
		ack  decode.ACKStatus
	}{
		{0x70, decode.DirectionWrite, 0x08, decode.ACK},
		{0x30, decode.DirectionWrite, 0x03, decode.ACK},
		{0x30, decode.DirectionRead, 0x15, decode.NACK},
	}
	for i, f := range res.Frames {
		if f.Address != want[i].addr || f.Direction != want[i].dir {
			t.Fatalf("frame %d = %+v", i, f)
		}
		if len(f.Data) != 1 || f.Data[0].Value != want[i].data || f.Data[0].Ack != want[i].ack {
			t.Fatalf("frame %d data = %+v", i, f.Data)
		}
	}
	if len(res.Timings) == 0 {
		t.Fatal("no timings")
	}
}

func TestReadBinMultipleWaveformsAndSegments(t *testing.T) {
	const points, headerSize = 2, 140
	var body []byte
	putHeader := func(label string, segment uint32) {
		h := make([]byte, headerSize)
		binary.LittleEndian.PutUint32(h[0:4], headerSize)
		binary.LittleEndian.PutUint32(h[4:8], 1) // normal
		binary.LittleEndian.PutUint32(h[8:12], 1)
		binary.LittleEndian.PutUint32(h[12:16], points)
		binary.LittleEndian.PutUint32(h[16:20], 1)
		binary.LittleEndian.PutUint64(h[32:40], math.Float64bits(1e-9))
		binary.LittleEndian.PutUint64(h[40:48], math.Float64bits(0))
		binary.LittleEndian.PutUint32(h[48:52], 2) // seconds
		binary.LittleEndian.PutUint32(h[52:56], 1) // volts
		copy(h[112:128], label)
		binary.LittleEndian.PutUint32(h[136:140], segment)
		body = append(body, h...)
		dh := make([]byte, 12)
		binary.LittleEndian.PutUint32(dh[0:4], 12)
		binary.LittleEndian.PutUint16(dh[4:6], 1)
		binary.LittleEndian.PutUint16(dh[6:8], 4)
		binary.LittleEndian.PutUint32(dh[8:12], points*4)
		body = append(body, dh...)
		for i := 0; i < points; i++ {
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, math.Float32bits(float32(i)))
			body = append(body, b...)
		}
	}
	putHeader("CH1", 0)
	putHeader("CH2", 1)
	file := make([]byte, 12)
	copy(file, "AG10")
	binary.LittleEndian.PutUint32(file[4:8], uint32(12+len(body)))
	binary.LittleEndian.PutUint32(file[8:12], 2)
	file = append(file, body...)

	cap, err := keysight.ReadBin(bytes.NewReader(file), int64(len(file)), keysight.BinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cap.Records) != 2 || cap.Records[0].Channel != "CH1" || cap.Records[1].SegmentIndex != 1 {
		t.Fatalf("records = %+v", cap.Records)
	}
	if w, ok := cap.Channel("CH2", 1); !ok || len(w.Samples) != 2 {
		t.Fatal("CH2/segment 1 missing")
	}
}
