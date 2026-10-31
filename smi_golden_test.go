package decode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

// The SMI golden vectors list each field explicitly and carry hand-written
// expected values. The waveform scheduler below is independent of
// testwave.MDIOBuilder.

type goldenSMIVector struct {
	Name                     string           `json:"name"`
	AllowPreambleSuppression bool             `json:"allow_preamble_suppression"`
	ExpectedFrames           *int             `json:"expected_frames"`
	Frames                   []goldenSMIFrame `json:"frames"`
	ResultDiagnostics        []string         `json:"result_diagnostics"`
}

type goldenSMIFrame struct {
	Preamble int             `json:"preamble"`
	ST       string          `json:"st"`
	OP       string          `json:"op"`
	PHY      string          `json:"phy"`
	REG      string          `json:"reg"`
	TA       string          `json:"ta"`
	Data     string          `json:"data"`
	Expect   goldenSMIExpect `json:"expect"`
}

type goldenSMIExpect struct {
	Direction    string   `json:"direction"`
	PHY          int      `json:"phy"`
	Reg          int      `json:"reg"`
	Data         int      `json:"data"`
	PreambleBits int      `json:"preamble_bits"`
	Turnaround   string   `json:"turnaround"`
	Complete     bool     `json:"complete"`
	DataValid    bool     `json:"data_valid"`
	Diagnostics  []string `json:"diagnostics"`
}

func TestSMIGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "smi", "golden_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors []goldenSMIVector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 6 {
		t.Fatalf("unexpectedly few golden vectors: %d", len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			mdc, mdio := buildGoldenSMI(t, v.Frames)
			opts := baseSMIOptions()
			opts.AllowPreambleSuppression = v.AllowPreambleSuppression
			res, err := decode.DecodeSMI(mdc, mdio, opts)
			if err != nil {
				t.Fatal(err)
			}
			wantFrames := len(v.Frames)
			if v.ExpectedFrames != nil {
				wantFrames = *v.ExpectedFrames
			}
			if len(res.Frames) != wantFrames {
				t.Fatalf("frames = %d, want %d: %+v", len(res.Frames), wantFrames, res.Diagnostics)
			}
			for i := 0; i < wantFrames; i++ {
				got, want := res.Frames[i], v.Frames[i].Expect
				if got.Direction.String() != want.Direction {
					t.Errorf("frame %d direction = %s, want %s", i, got.Direction, want.Direction)
				}
				if int(got.PHYAddr) != want.PHY || int(got.RegAddr) != want.Reg {
					t.Errorf("frame %d phy/reg = %#x/%#x, want %#x/%#x", i, got.PHYAddr, got.RegAddr, want.PHY, want.Reg)
				}
				if int(got.Data) != want.Data {
					t.Errorf("frame %d data = %#x, want %#x", i, got.Data, want.Data)
				}
				if got.PreambleBits != want.PreambleBits {
					t.Errorf("frame %d preamble = %d, want %d", i, got.PreambleBits, want.PreambleBits)
				}
				if got.Turnaround.String() != want.Turnaround {
					t.Errorf("frame %d turnaround = %s, want %s", i, got.Turnaround, want.Turnaround)
				}
				if got.Complete != want.Complete || got.DataValid != want.DataValid {
					t.Errorf("frame %d flags = complete:%v data_valid:%v, want %v/%v",
						i, got.Complete, got.DataValid, want.Complete, want.DataValid)
				}
				for _, code := range want.Diagnostics {
					if !hasDiag(got.Diagnostics, code) {
						t.Errorf("frame %d missing diagnostic %q: %+v", i, code, got.Diagnostics)
					}
				}
			}
			for _, code := range v.ResultDiagnostics {
				if !hasDiag(res.Diagnostics, code) {
					t.Errorf("missing result diagnostic %q: %+v", code, res.Diagnostics)
				}
			}
		})
	}
}

func buildGoldenSMI(t *testing.T, frames []goldenSMIFrame) (decode.Waveform, decode.Waveform) {
	t.Helper()
	const (
		period = 1e-6
		high   = 0.5e-6
		low    = 0.5e-6
		hold   = 0.2e-6
	)
	var bits []bool
	for _, f := range frames {
		for i := 0; i < f.Preamble; i++ {
			bits = append(bits, true)
		}
		bits = append(bits, parseBits(t, f.ST, 2, 2)...)
		bits = append(bits, parseBits(t, f.OP, 2, 2)...)
		bits = append(bits, parseBits(t, f.PHY, 5, 5)...)
		bits = append(bits, parseBits(t, f.REG, 5, 5)...)
		bits = append(bits, parseBits(t, f.TA, 2, 2)...)
		if len(f.Data) > 0 {
			bits = append(bits, parseBits(t, f.Data, 1, 16)...)
		}
	}
	if len(bits) == 0 {
		t.Fatal("golden vector has no bits")
	}

	var mdc, mdio []testwave.Transition
	cycle := 0.0
	mdc = append(mdc, testwave.Transition{T: cycle, High: false})
	mdioHigh := true
	lastRise := 0.0
	for _, bit := range bits {
		if bit != mdioHigh {
			mdio = append(mdio, testwave.Transition{T: cycle + hold, High: bit})
			mdioHigh = bit
		}
		lastRise = cycle + low
		mdc = append(mdc, testwave.Transition{T: lastRise, High: true})
		mdc = append(mdc, testwave.Transition{T: cycle + period, High: false})
		cycle += period
	}
	duration := lastRise + 0.3*period
	cfg := defaultWaveConfig(0, 200e6, duration)
	mdcWave, err := testwave.Build(cfg, true, mdc)
	if err != nil {
		t.Fatal(err)
	}
	mdioWave, err := testwave.Build(cfg, true, mdio)
	if err != nil {
		t.Fatal(err)
	}
	return mdcWave, mdioWave
}

func parseBits(t *testing.T, s string, minLen, maxLen int) []bool {
	t.Helper()
	if len(s) < minLen || len(s) > maxLen {
		t.Fatalf("bit string %q has length %d, want %d..%d", s, len(s), minLen, maxLen)
	}
	bits := make([]bool, len(s))
	for i, c := range s {
		switch c {
		case '0':
			bits[i] = false
		case '1':
			bits[i] = true
		default:
			t.Fatalf("bad bit %q in %q", c, s)
		}
	}
	return bits
}
