package decode_test

import (
	"errors"
	"math"
	"testing"

	"github.com/jkbhuan/go-decode"
)

func testPreamble() decode.KeysightPreamble {
	return decode.KeysightPreamble{
		XIncrement: 2e-9,
		XOrigin:    -1e-6,
		XReference: 0,
		YIncrement: 0.01,
		YOrigin:    0,
		YReference: 128,
	}
}

func TestFormatKeysightByteUnsigned(t *testing.T) {
	payload := []byte{0, 128, 255}
	w, err := decode.FormatKeysightWaveform(payload, testPreamble(), decode.SampleEncoding{
		BytesPerPoint: 1, Signed: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, w.SampleRate, 5e8, 1e-6, "sample rate")
	approx(t, w.StartTime, -1e-6, 1e-18, "start time")
	want := []float64{-1.28, 0, 1.27}
	for i := range want {
		approx(t, float64(w.Samples[i]), want[i], 1e-6, "sample")
	}
	tt, _ := w.TimeAt(2)
	approx(t, tt, -1e-6+2*2e-9, 1e-18, "TimeAt")
}

func TestFormatKeysightByteSigned(t *testing.T) {
	p := testPreamble()
	p.YReference = 0
	payload := []byte{0x80, 0x00, 0x7f} // -128, 0, 127
	w, err := decode.FormatKeysightWaveform(payload, p, decode.SampleEncoding{
		BytesPerPoint: 1, Signed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{-1.28, 0, 1.27}
	for i := range want {
		approx(t, float64(w.Samples[i]), want[i], 1e-6, "signed sample")
	}
}

func TestFormatKeysightWordByteOrder(t *testing.T) {
	p := testPreamble()
	p.YReference = 0
	p.YIncrement = 1

	little, err := decode.FormatKeysightWaveform([]byte{0x02, 0x01}, p, decode.SampleEncoding{
		BytesPerPoint: 2, Signed: false, ByteOrder: "LSBFirst",
	})
	if err != nil {
		t.Fatal(err)
	}
	big, err := decode.FormatKeysightWaveform([]byte{0x01, 0x02}, p, decode.SampleEncoding{
		BytesPerPoint: 2, Signed: false, ByteOrder: "MSBFirst",
	})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, float64(little.Samples[0]), 258, 1e-6, "LSBFirst")
	approx(t, float64(big.Samples[0]), 258, 1e-6, "MSBFirst")

	// Instruments often return the abbreviations LSBF/MSBF.
	abbrev, err := decode.FormatKeysightWaveform([]byte{0x02, 0x01}, p, decode.SampleEncoding{
		BytesPerPoint: 2, Signed: false, ByteOrder: "LSBF",
	})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, float64(abbrev.Samples[0]), 258, 1e-6, "LSBF")
	if _, err := decode.FormatKeysightWaveform([]byte{0x01, 0x02}, p, decode.SampleEncoding{
		BytesPerPoint: 2, Signed: false, ByteOrder: "MSBF",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFormatKeysightWordSigned(t *testing.T) {
	p := testPreamble()
	p.YReference = 0
	p.YIncrement = 1
	w, err := decode.FormatKeysightWaveform([]byte{0xff, 0xff}, p, decode.SampleEncoding{
		BytesPerPoint: 2, Signed: true, ByteOrder: "big",
	})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, float64(w.Samples[0]), -1, 1e-6, "signed word")
}

func TestFormatKeysightStartTimeWithXReference(t *testing.T) {
	p := testPreamble()
	p.XOrigin = 1e-3
	p.XReference = 100
	w, err := decode.FormatKeysightWaveform([]byte{128}, p, decode.SampleEncoding{BytesPerPoint: 1})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, w.StartTime, 1e-3-100*2e-9, 1e-18, "XReference start time")
}

func TestFormatKeysightValidation(t *testing.T) {
	enc := decode.SampleEncoding{BytesPerPoint: 1}

	badP := testPreamble()
	badP.XIncrement = 0
	if _, err := decode.FormatKeysightWaveform([]byte{1}, badP, enc); !errors.Is(err, decode.ErrInvalidPreamble) {
		t.Fatalf("zero XIncrement: %v", err)
	}
	badP = testPreamble()
	badP.XIncrement = -1
	if _, err := decode.FormatKeysightWaveform([]byte{1}, badP, enc); !errors.Is(err, decode.ErrInvalidPreamble) {
		t.Fatalf("negative XIncrement: %v", err)
	}
	badP = testPreamble()
	badP.YIncrement = 0
	if _, err := decode.FormatKeysightWaveform([]byte{1}, badP, enc); !errors.Is(err, decode.ErrInvalidPreamble) {
		t.Fatalf("zero YIncrement: %v", err)
	}
	badP = testPreamble()
	badP.YOrigin = math.NaN()
	if _, err := decode.FormatKeysightWaveform([]byte{1}, badP, enc); !errors.Is(err, decode.ErrInvalidPreamble) {
		t.Fatalf("NaN preamble: %v", err)
	}

	if _, err := decode.FormatKeysightWaveform([]byte{1, 2, 3}, testPreamble(), decode.SampleEncoding{BytesPerPoint: 2, ByteOrder: "big"}); !errors.Is(err, decode.ErrPayloadLength) {
		t.Fatalf("length mismatch: %v", err)
	}
	if _, err := decode.FormatKeysightWaveform(nil, testPreamble(), enc); !errors.Is(err, decode.ErrPayloadLength) {
		t.Fatalf("empty payload: %v", err)
	}
	if _, err := decode.FormatKeysightWaveform([]byte{1, 2}, testPreamble(), decode.SampleEncoding{BytesPerPoint: 2, ByteOrder: "sideways"}); !errors.Is(err, decode.ErrInvalidEncoding) {
		t.Fatalf("bad byte order: %v", err)
	}
	if _, err := decode.FormatKeysightWaveform([]byte{1, 2, 3}, testPreamble(), decode.SampleEncoding{BytesPerPoint: 3}); !errors.Is(err, decode.ErrInvalidEncoding) {
		t.Fatalf("bad bytes per point: %v", err)
	}
	if _, err := decode.FormatKeysightWaveform([]byte{1}, testPreamble(), decode.SampleEncoding{BytesPerPoint: 1, ByteOrder: "sideways"}); !errors.Is(err, decode.ErrInvalidEncoding) {
		t.Fatalf("bad byte order with one byte: %v", err)
	}
}

func TestParseKeysightPreamble(t *testing.T) {
	p, info, err := decode.ParseKeysightPreamble("0,0,2000,1,2.00000000E-09,-1.00000000E-06,0,1.00000000E-02,0,128")
	if err != nil {
		t.Fatal(err)
	}
	approx(t, p.XIncrement, 2e-9, 1e-20, "XIncrement")
	approx(t, p.XOrigin, -1e-6, 1e-20, "XOrigin")
	approx(t, p.YReference, 128, 1e-9, "YReference")
	if info.Format != 0 || info.Points != 2000 || info.Count != 1 {
		t.Fatalf("info = %+v", info)
	}
	if _, _, err := decode.ParseKeysightPreamble("0,0,2000"); !errors.Is(err, decode.ErrInvalidPreamble) {
		t.Fatalf("short preamble: %v", err)
	}
	// Infiniium returns extra fields after the ten common ones; they must be
	// accepted and ignored.
	p2, info2, err := decode.ParseKeysightPreamble("1,1,100,1,1e-9,0,0,1e-3,0,0,1,1e-6,0,1,0,01 Apr 2026,12:00:00:00,0,0,1,sec,V,1e9,1e6")
	if err != nil {
		t.Fatalf("Infiniium preamble: %v", err)
	}
	approx(t, p2.XIncrement, 1e-9, 1e-20, "Infiniium XIncrement")
	if info2.Points != 100 {
		t.Fatalf("Infiniium info = %+v", info2)
	}
	if _, _, err := decode.ParseKeysightPreamble("0,0,2000,1,-1,-1,0,1,0,0"); !errors.Is(err, decode.ErrInvalidPreamble) {
		t.Fatalf("negative increment: %v", err)
	}
}

func TestParseIEEE4882DefiniteBlock(t *testing.T) {
	data := append([]byte("#42000"), make([]byte, 2000)...)
	data = append(data, 'X', 'Y')
	payload, rest, err := decode.ParseIEEE4882DefiniteBlock(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2000 || len(rest) != 2 || string(rest) != "XY" {
		t.Fatalf("payload=%d rest=%q", len(payload), rest)
	}

	bad := [][]byte{
		[]byte("123"),
		[]byte("#"),
		[]byte("#0abc"),
		[]byte("#3ab"),
		[]byte("#45" + "00"),
	}
	for _, b := range bad {
		if _, _, err := decode.ParseIEEE4882DefiniteBlock(b); !errors.Is(err, decode.ErrInvalidBlock) {
			t.Errorf("%q: got %v, want ErrInvalidBlock", b, err)
		}
	}
}
