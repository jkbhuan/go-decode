package decode_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkbhuan/go-decode"
)

// The golden files are test data, so their JSON field names live in
// test-only DTOs; the decode package itself has no JSON encoding contract.
type goldenPreamble struct {
	XIncrement float64 `json:"x_increment_s"`
	XOrigin    float64 `json:"x_origin_s"`
	XReference float64 `json:"x_reference"`
	YIncrement float64 `json:"y_increment_v"`
	YOrigin    float64 `json:"y_origin_v"`
	YReference float64 `json:"y_reference"`
}

type goldenEncoding struct {
	BytesPerPoint int    `json:"bytes_per_point"`
	Signed        bool   `json:"signed"`
	ByteOrder     string `json:"byte_order"`
}

type goldenKeysightVector struct {
	Name     string         `json:"name"`
	Payload  string         `json:"payload_hex"`
	Preamble goldenPreamble `json:"preamble"`
	Encoding goldenEncoding `json:"encoding"`
	Expect   struct {
		SampleRate float64   `json:"sample_rate"`
		StartTime  float64   `json:"start_time"`
		Samples    []float32 `json:"samples"`
	} `json:"expect"`
}

func TestKeysightGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "keysight", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors []goldenKeysightVector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 5 {
		t.Fatalf("unexpectedly few vectors: %d", len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			payload, err := hex.DecodeString(v.Payload)
			if err != nil {
				t.Fatal(err)
			}
			p := decode.KeysightPreamble{
				XIncrement: v.Preamble.XIncrement,
				XOrigin:    v.Preamble.XOrigin,
				XReference: v.Preamble.XReference,
				YIncrement: v.Preamble.YIncrement,
				YOrigin:    v.Preamble.YOrigin,
				YReference: v.Preamble.YReference,
			}
			enc := decode.SampleEncoding{
				BytesPerPoint: v.Encoding.BytesPerPoint,
				Signed:        v.Encoding.Signed,
				ByteOrder:     v.Encoding.ByteOrder,
			}
			w, err := decode.FormatKeysightWaveform(payload, p, enc)
			if err != nil {
				t.Fatal(err)
			}
			approx(t, w.SampleRate, v.Expect.SampleRate, 1e-3, "sample rate")
			approx(t, w.StartTime, v.Expect.StartTime, 1e-18, "start time")
			if len(w.Samples) != len(v.Expect.Samples) {
				t.Fatalf("samples = %d, want %d", len(w.Samples), len(v.Expect.Samples))
			}
			for i, want := range v.Expect.Samples {
				approx(t, float64(w.Samples[i]), float64(want), 1e-6, "sample")
			}
		})
	}
}
