package decode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

// The I2C golden vectors are hand-written bit strings with hand-written
// expected fields. They are scheduled by the small interpreter below, which is
// deliberately separate from testwave.I2CBuilder, so a bug in the builder
// cannot mask a decoder bug.

type goldenI2CVectors struct {
	Name   string          `json:"name"`
	Tokens string          `json:"tokens"`
	Expect goldenI2CExpect `json:"expect"`
}

type goldenI2CExpect struct {
	Transactions []goldenI2CTransaction `json:"transactions"`
	Diagnostics  []string               `json:"diagnostics"`
}

type goldenI2CTransaction struct {
	Complete    bool             `json:"complete"`
	Diagnostics []string         `json:"diagnostics"`
	Frames      []goldenI2CFrame `json:"frames"`
}

type goldenI2CFrame struct {
	Address       int      `json:"address"`
	AddressBits   int      `json:"address_bits"`
	Direction     string   `json:"direction"`
	AddressACK    string   `json:"address_ack"`
	RepeatedStart bool     `json:"repeated_start"`
	HasStop       bool     `json:"has_stop"`
	Data          []int    `json:"data"`
	DataACKs      []string `json:"data_acks"`
}

func TestI2CGoldenVectors(t *testing.T) {
	path := filepath.Join("testdata", "i2c", "golden_vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vectors []goldenI2CVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 5 {
		t.Fatalf("unexpectedly few golden vectors: %d", len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			scl, sda := buildGoldenI2C(t, v.Tokens)
			res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Transactions) != len(v.Expect.Transactions) {
				t.Fatalf("transactions = %d, want %d: %+v", len(res.Transactions), len(v.Expect.Transactions), res.Diagnostics)
			}
			for i, wantTx := range v.Expect.Transactions {
				got := res.Transactions[i]
				if got.Complete != wantTx.Complete {
					t.Errorf("tx %d complete = %v, want %v (%+v)", i, got.Complete, wantTx.Complete, got.Diagnostics)
				}
				for _, code := range wantTx.Diagnostics {
					if !hasDiag(got.Diagnostics, code) {
						t.Errorf("tx %d missing diagnostic %q: %+v", i, code, got.Diagnostics)
					}
				}
				if len(got.Frames) != len(wantTx.Frames) {
					t.Fatalf("tx %d frames = %d, want %d", i, len(got.Frames), len(wantTx.Frames))
				}
				for f, wantF := range wantTx.Frames {
					gf := got.Frames[f]
					if int(gf.Address) != wantF.Address || int(gf.AddressBits) != wantF.AddressBits {
						t.Errorf("tx %d frame %d address = %#x/%d, want %#x/%d",
							i, f, gf.Address, gf.AddressBits, wantF.Address, wantF.AddressBits)
					}
					if gf.Direction.String() != wantF.Direction {
						t.Errorf("tx %d frame %d direction = %s, want %s", i, f, gf.Direction, wantF.Direction)
					}
					if gf.AddressACK.String() != wantF.AddressACK {
						t.Errorf("tx %d frame %d address ACK = %s, want %s", i, f, gf.AddressACK, wantF.AddressACK)
					}
					if gf.RepeatedStart != wantF.RepeatedStart || gf.HasStop != wantF.HasStop {
						t.Errorf("tx %d frame %d flags = %+v, want repeated=%v stop=%v",
							i, f, gf, wantF.RepeatedStart, wantF.HasStop)
					}
					if len(gf.Data) != len(wantF.Data) {
						t.Fatalf("tx %d frame %d data len = %d, want %d (%+v)",
							i, f, len(gf.Data), len(wantF.Data), gf.Data)
					}
					for d := range wantF.Data {
						if int(gf.Data[d].Value) != wantF.Data[d] {
							t.Errorf("tx %d frame %d data[%d] = %#x, want %#x", i, f, d, gf.Data[d].Value, wantF.Data[d])
						}
						if d < len(wantF.DataACKs) && gf.Data[d].Ack.String() != wantF.DataACKs[d] {
							t.Errorf("tx %d frame %d data[%d] ACK = %s, want %s",
								i, f, d, gf.Data[d].Ack, wantF.DataACKs[d])
						}
					}
				}
			}
			for _, code := range v.Expect.Diagnostics {
				if !hasDiag(res.Diagnostics, code) {
					t.Errorf("missing result diagnostic %q: %+v", code, res.Diagnostics)
				}
			}
		})
	}
}

// buildGoldenI2C interprets the compact token language:
//
//	S = START, R = repeated START, P = STOP, I = idle, A = ACK bit (low),
//	N = NACK bit (high), 0/1 = one data bit each.
func buildGoldenI2C(t *testing.T, tokens string) (decode.Waveform, decode.Waveform) {
	t.Helper()
	const (
		lowTime    = 5e-6
		highTime   = 2e-6
		dataHold   = 1e-6
		startHold  = 0.5e-6
		startSetup = 1.5e-6
		stopSetup  = 0.8e-6
	)
	s := &goldenSched{t: 2e-6, sclHigh: true, sdaHigh: true}
	for _, tok := range strings.Fields(tokens) {
		switch tok {
		case "S":
			s.setSDA(false, s.t)
			fall := s.t + startHold
			s.setSCL(false, fall)
			s.t = fall
		case "R":
			s.setSDA(true, s.t+dataHold)
			rise := s.t + lowTime
			s.setSCL(true, rise)
			sr := rise + startSetup
			s.setSDA(false, sr)
			fall := sr + startHold
			s.setSCL(false, fall)
			s.t = fall
		case "P":
			if s.sdaHigh {
				s.setSDA(false, s.t+dataHold)
			}
			rise := s.t + lowTime
			s.setSCL(true, rise)
			stop := rise + stopSetup
			s.setSDA(true, stop)
			s.t = stop
		case "I":
			s.t += 3e-6
		case "A":
			s.bit(false, lowTime, highTime, dataHold)
		case "N":
			s.bit(true, lowTime, highTime, dataHold)
		default:
			for _, c := range tok {
				switch c {
				case '0':
					s.bit(false, lowTime, highTime, dataHold)
				case '1':
					s.bit(true, lowTime, highTime, dataHold)
				default:
					t.Fatalf("bad golden token %q", tok)
				}
			}
		}
	}
	cfg := defaultWaveConfig(0, 200e6, s.t+5e-6)
	scl, err := testwave.Build(cfg, true, s.scl)
	if err != nil {
		t.Fatal(err)
	}
	sda, err := testwave.Build(cfg, true, s.sda)
	if err != nil {
		t.Fatal(err)
	}
	return scl, sda
}

type goldenSched struct {
	t       float64
	sclHigh bool
	sdaHigh bool
	scl     []testwave.Transition
	sda     []testwave.Transition
}

func (s *goldenSched) setSCL(high bool, at float64) {
	if high == s.sclHigh {
		return
	}
	s.scl = append(s.scl, testwave.Transition{T: at, High: high})
	s.sclHigh = high
}

func (s *goldenSched) setSDA(high bool, at float64) {
	if high == s.sdaHigh {
		return
	}
	s.sda = append(s.sda, testwave.Transition{T: at, High: high})
	s.sdaHigh = high
}

func (s *goldenSched) bit(high bool, low, highDur, hold float64) {
	if high != s.sdaHigh {
		s.setSDA(high, s.t+hold)
	}
	rise := s.t + low
	s.setSCL(true, rise)
	fall := rise + highDur
	s.setSCL(false, fall)
	s.t = fall
}
