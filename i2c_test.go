package decode_test

import (
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

func newTestI2C(t *testing.T, start float64) *testwave.I2CBuilder {
	t.Helper()
	b := mustI2CBuilder(t, testwave.I2CConfig{
		Wave:        defaultWaveConfig(start, 200e6, 0),
		SCLHighTime: 2e-6,
		SCLLowTime:  5e-6,
		DataHold:    1e-6,
		DataSetup:   4e-6,
		StartHold:   0.5e-6,
		StartSetup:  1.5e-6,
		StopSetup:   0.8e-6,
	})
	// Leave an idle high period so the record starts on a valid idle bus and
	// the first START is actually captured.
	b.Idle(1e-6)
	return b
}

func TestI2CWriteTransaction(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true) // 0x50 write
	b.Byte(0x12, true)
	b.Byte(0x34, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d, want 1", len(res.Transactions))
	}
	tx := res.Transactions[0]
	if !tx.Complete {
		t.Fatalf("transaction not complete: %+v", tx.Diagnostics)
	}
	if len(tx.Frames) != 1 {
		t.Fatalf("frames = %d", len(tx.Frames))
	}
	f := tx.Frames[0]
	if f.Address != 0x50 || f.AddressBits != 7 || f.Direction != decode.DirectionWrite {
		t.Fatalf("frame = %+v", f)
	}
	if f.AddressACK != decode.ACK {
		t.Fatalf("address ACK = %s", f.AddressACK)
	}
	if !f.HasStop || !f.Complete || f.RepeatedStart {
		t.Fatalf("frame flags = %+v", f)
	}
	if len(f.Data) != 2 || f.Data[0].Value != 0x12 || f.Data[1].Value != 0x34 {
		t.Fatalf("data = %+v", f.Data)
	}
	for _, d := range f.Data {
		if d.Ack != decode.ACK {
			t.Fatalf("data ACK = %s", d.Ack)
		}
	}
	if len(tx.Timings) == 0 {
		t.Fatalf("expected timing candidates: %+v", tx.Diagnostics)
	}
	if hasDiag(tx.Diagnostics, decode.CodeI2CStopMidByte) || hasDiag(tx.Diagnostics, decode.CodeI2CStartMidByte) {
		t.Fatalf("normal STOP produced a mid-byte diagnostic: %+v", tx.Diagnostics)
	}
}

func TestI2CReadTransactionWithFinalNACK(t *testing.T) {
	b := newTestI2C(t, -3e-6)
	b.Start()
	b.Byte(0xA1, true) // 0x50 read
	b.Byte(0x55, true)
	b.Byte(0xAA, false) // last byte NACKed by master
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d", len(res.Transactions))
	}
	f := res.Transactions[0].Frames[0]
	if f.Direction != decode.DirectionRead {
		t.Fatalf("direction = %s", f.Direction)
	}
	if len(f.Data) != 2 || f.Data[0].Ack != decode.ACK || f.Data[1].Ack != decode.NACK {
		t.Fatalf("data = %+v", f.Data)
	}
	if !res.Transactions[0].Complete {
		t.Fatalf("transaction should be complete despite final NACK: %+v", res.Transactions[0].Diagnostics)
	}
	// The negative StartTime must survive: the transaction starts after -2 us.
	if res.Transactions[0].Start < -3e-6 || res.Transactions[0].Start > -1e-6 {
		t.Fatalf("transaction start = %g, expected absolute negative time", res.Transactions[0].Start)
	}
	tm, ok := decode.SelectTiming(res.Transactions[0].Timings, decode.TimingFilter{Name: decode.TimingHDSTA}, decode.SelectFirst)
	if !ok {
		t.Fatalf("no tHD;STA candidate: %+v", res.Transactions[0].Diagnostics)
	}
	if tm.X1 > -1e-6 || tm.X2 > -1e-6 {
		t.Fatalf("cursor coordinates were re-zeroed: %+v", tm)
	}
	checkTimingInvariant(t, tm)
}

func TestI2CCombinedReadKeepsFramesWithoutOffsetAssumption(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true) // write address
	b.Byte(0x10, true) // register value byte; must remain raw data
	b.RepeatedStart()
	b.Byte(0xA1, true) // read address
	b.Byte(0x5A, false)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("combined read must be one transaction, got %d", len(res.Transactions))
	}
	tx := res.Transactions[0]
	if len(tx.Frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(tx.Frames))
	}
	f0, f1 := tx.Frames[0], tx.Frames[1]
	if f0.Direction != decode.DirectionWrite || f1.Direction != decode.DirectionRead {
		t.Fatalf("directions = %s, %s", f0.Direction, f1.Direction)
	}
	if len(f0.Data) != 1 || f0.Data[0].Value != 0x10 {
		t.Fatalf("write-phase data = %+v", f0.Data)
	}
	if !f1.RepeatedStart || f1.Address != 0x50 {
		t.Fatalf("read frame = %+v", f1)
	}
	if len(f1.Data) != 1 || f1.Data[0].Value != 0x5A || f1.Data[0].Ack != decode.NACK {
		t.Fatalf("read-phase data = %+v", f1.Data)
	}
	if hasDiag(tx.Diagnostics, decode.CodeI2CStartMidByte) {
		t.Fatalf("normal repeated START produced a mid-byte diagnostic: %+v", tx.Diagnostics)
	}
}

func TestI2CMultipleTransactionsAndTBUF(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x01, true)
	b.Stop()
	b.Idle(10e-6)
	b.Start()
	b.Byte(0xA1, true)
	b.Byte(0x02, false)
	b.Stop()
	b.Idle(12e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x03, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	opts := baseI2COptions()
	res, err := decode.DecodeI2C(scl, sda, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 3 {
		t.Fatalf("transactions = %d, want 3: %+v", len(res.Transactions), res.Diagnostics)
	}
	for i, tx := range res.Transactions {
		if !tx.Complete {
			t.Fatalf("tx %d incomplete: %+v", i, tx.Diagnostics)
		}
	}

	bufs := findTiming(res.Transactions[1].Timings, decode.TimingBUF)
	if len(bufs) != 1 {
		t.Fatalf("tBUF candidates on tx2 = %d, want 1", len(bufs))
	}
	approx(t, bufs[0].Value, 10e-6, 2.0/200e6, "tBUF tx2")
	checkTimingInvariant(t, bufs[0])

	bufs = findTiming(res.Transactions[2].Timings, decode.TimingBUF)
	if len(bufs) != 1 {
		t.Fatalf("tBUF candidates on tx3 = %d, want 1", len(bufs))
	}
	approx(t, bufs[0].Value, 12e-6, 2.0/200e6, "tBUF tx3")

	if got := findTiming(res.Transactions[0].Timings, decode.TimingBUF); len(got) != 0 {
		t.Fatalf("first transaction must not have tBUF: %+v", got)
	}
}

func TestI2CClockStretching(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.StretchLow(7e-6)
	b.Byte(0x42, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 || len(res.Transactions[0].Frames) != 1 {
		t.Fatalf("unexpected transactions: %+v", res.Transactions)
	}
	if got := res.Transactions[0].Frames[0].Data; len(got) != 1 || got[0].Value != 0x42 {
		t.Fatalf("stretched-clock data = %+v", got)
	}
	lows := findTiming(res.Transactions[0].Timings, decode.TimingTLOW)
	maxLow := 0.0
	for _, l := range lows {
		if l.Value > maxLow {
			maxLow = l.Value
		}
	}
	if maxLow < 7e-6 {
		t.Fatalf("clock stretching not reflected in tLOW: max=%g, candidates=%+v", maxLow, lows)
	}
}

func TestI2CTruncatedMidByte(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Bits([]bool{true, false, true, false}) // 4 data bits, then capture ends
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d", len(res.Transactions))
	}
	tx := res.Transactions[0]
	if tx.Complete {
		t.Fatal("truncated transaction must not be complete")
	}
	f := tx.Frames[0]
	if f.AddressBits != 7 || f.Address != 0x50 || f.AddressACK != decode.ACK {
		t.Fatalf("proven address part lost: %+v", f)
	}
	if len(f.Data) != 0 {
		t.Fatalf("partial byte must not be reported: %+v", f.Data)
	}
	if !hasDiag(tx.Diagnostics, decode.CodeI2CTruncatedByte) {
		t.Fatalf("missing truncated-byte diagnostic: %+v", tx.Diagnostics)
	}
	if !hasDiag(tx.Diagnostics, decode.CodeI2CMissingStop) {
		t.Fatalf("missing missing-stop diagnostic: %+v", tx.Diagnostics)
	}
}

func TestI2CTruncatedBeforeACK(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Bits([]bool{false, true, false, true, false, true, false, true}) // 0x55, no ACK
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	tx := res.Transactions[0]
	f := tx.Frames[0]
	if len(f.Data) != 1 || f.Data[0].Value != 0x55 {
		t.Fatalf("captured data byte missing: %+v", f.Data)
	}
	if f.Data[0].Ack != decode.ACKUnknown {
		t.Fatalf("ACK should be unknown, got %s", f.Data[0].Ack)
	}
	if f.Complete || tx.Complete {
		t.Fatal("frame/transaction must be incomplete")
	}
	if !hasDiag(tx.Diagnostics, decode.CodeI2CMissingACK) {
		t.Fatalf("missing ACK diagnostic: %+v", tx.Diagnostics)
	}
}

func TestI2C10BitAddressingIsExplicitlyUnsupported(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xF0, true)
	b.Byte(0x12, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d", len(res.Transactions))
	}
	f := res.Transactions[0].Frames[0]
	if f.AddressBits != 0 || f.Address != 0 || f.Direction != decode.DirectionUnknown {
		t.Fatalf("10-bit prefix not rejected: %+v", f)
	}
	if f.Complete {
		t.Fatal("unsupported frame must not be marked complete")
	}
	if !hasDiag(res.Transactions[0].Diagnostics, decode.CodeI2C10BitUnsupported) {
		t.Fatalf("missing 10-bit diagnostic: %+v", res.Transactions[0].Diagnostics)
	}
}

func TestI2CNoTimingThresholdsYieldsNoFabricatedValues(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x01, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	opts := decode.I2COptions{
		SCLThreshold: decode.SingleThreshold(testLogic),
		SDAThreshold: decode.SingleThreshold(testLogic),
	}
	res, err := decode.DecodeI2C(scl, sda, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d", len(res.Transactions))
	}
	if len(res.Transactions[0].Timings) != 0 || len(res.Transactions[0].SCLFrequencies) != 0 {
		t.Fatal("timing values must not be generated without timing thresholds")
	}
	if !hasDiag(res.Diagnostics, decode.CodeI2CTimingThresholdUnavailable) {
		t.Fatalf("missing threshold diagnostic: %+v", res.Diagnostics)
	}
}

func TestI2CTimingValuesAndCursors(t *testing.T) {
	cfg := testwave.I2CConfig{
		Wave:        defaultWaveConfig(0, 200e6, 0),
		SCLHighTime: 2e-6,
		SCLLowTime:  5e-6,
		DataHold:    1e-6,
		DataSetup:   4e-6,
		StartHold:   0.5e-6,
		StartSetup:  1.5e-6,
		StopSetup:   0.8e-6,
	}
	b := mustI2CBuilder(t, cfg)
	b.Idle(1e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x12, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d", len(res.Transactions))
	}
	tx := res.Transactions[0]
	tol := 2.0 / cfg.Wave.SampleRate

	hdstas := findTiming(tx.Timings, decode.TimingHDSTA)
	if len(hdstas) != 1 {
		t.Fatalf("tHD;STA candidates = %d", len(hdstas))
	}
	approx(t, hdstas[0].Value, 0.5e-6, tol, "tHD;STA")
	approx(t, hdstas[0].X1, 1e-6, tol, "tHD;STA X1 (absolute start)")
	approx(t, hdstas[0].X2, 1.5e-6, tol, "tHD;STA X2")

	// No repeated START: tSU;STA must be absent, with a diagnostic.
	if got := findTiming(tx.Timings, decode.TimingSUSTA); len(got) != 0 {
		t.Fatalf("initial START must not get tSU;STA: %+v", got)
	}
	if !hasDiag(tx.Diagnostics, decode.CodeTimingNotObservable) {
		t.Fatalf("missing not-observable diagnostic: %+v", tx.Diagnostics)
	}

	stos := findTiming(tx.Timings, decode.TimingSUSTO)
	if len(stos) != 1 {
		t.Fatalf("tSU;STO candidates = %d", len(stos))
	}
	approx(t, stos[0].Value, 0.8e-6, tol, "tSU;STO")

	highs := findTiming(tx.Timings, decode.TimingTHIGH)
	if len(highs) == 0 {
		t.Fatal("no tHIGH candidates")
	}
	for _, h := range highs {
		approx(t, h.Value, 2e-6, tol, "tHIGH")
	}
	lows := findTiming(tx.Timings, decode.TimingTLOW)
	if len(lows) == 0 {
		t.Fatal("no tLOW candidates")
	}
	for _, l := range lows {
		approx(t, l.Value, 5e-6, tol, "tLOW")
	}

	sudats := findTiming(tx.Timings, decode.TimingSUDAT)
	if len(sudats) == 0 {
		t.Fatalf("no tSU;DAT candidates: %+v", tx.Diagnostics)
	}
	for _, s := range sudats {
		approx(t, s.Value, 4e-6, tol, "tSU;DAT")
		checkTimingInvariant(t, s)
		if s.Phase != "address" && s.Phase != "data" && s.Phase != "ack" {
			t.Fatalf("unexpected phase %q", s.Phase)
		}
	}

	hdats := findTiming(tx.Timings, decode.TimingHDDAT)
	if len(hdats) == 0 {
		t.Fatalf("no tHD;DAT candidates: %+v", tx.Diagnostics)
	}
	for _, s := range hdats {
		approx(t, s.Value, 1e-6, tol, "tHD;DAT")
		checkTimingInvariant(t, s)
	}

	if len(tx.SCLFrequencies) == 0 {
		t.Fatal("no fSCL candidates")
	}
	for _, f := range tx.SCLFrequencies {
		approx(t, f.Value, 1/(7e-6), 1/(7e-6)*0.02, "fSCL")
	}

	for _, tm := range tx.Timings {
		checkTimingInvariant(t, tm)
	}
}

func TestI2CRepeatedStartSetupTime(t *testing.T) {
	cfg := testwave.I2CConfig{
		Wave:        defaultWaveConfig(0, 200e6, 0),
		SCLHighTime: 2e-6,
		SCLLowTime:  5e-6,
		DataHold:    1e-6,
		DataSetup:   4e-6,
		StartHold:   0.5e-6,
		StartSetup:  1.5e-6,
		StopSetup:   0.8e-6,
	}
	b := mustI2CBuilder(t, cfg)
	b.Idle(1e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x10, true)
	b.RepeatedStart()
	b.Byte(0xA1, true)
	b.Byte(0x55, false)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	tx := res.Transactions[0]
	sus := findTiming(tx.Timings, decode.TimingSUSTA)
	if len(sus) != 1 {
		t.Fatalf("tSU;STA candidates = %d, want 1: %+v", len(sus), tx.Diagnostics)
	}
	approx(t, sus[0].Value, 1.5e-6, 2.0/cfg.Wave.SampleRate, "tSU;STA")
	if sus[0].Phase != "start" {
		t.Fatalf("phase = %q", sus[0].Phase)
	}
}

func TestI2CNoiseAndSlowEdgesStillDecode(t *testing.T) {
	cfg := testwave.I2CConfig{
		Wave:        defaultWaveConfig(0, 200e6, 0),
		SCLHighTime: 3e-6,
		SCLLowTime:  6e-6,
		DataHold:    2e-6,
		DataSetup:   4e-6,
		StartHold:   1e-6,
		StartSetup:  2e-6,
		StopSetup:   1.5e-6,
	}
	cfg.Wave.RiseTime = 100e-9
	cfg.Wave.FallTime = 100e-9
	cfg.Wave.Noise = 0.15
	cfg.Wave.Seed = 42
	b := mustI2CBuilder(t, cfg)
	b.Idle(1e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x3C, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	opts := decode.I2COptions{
		SCLThreshold: decode.LogicThreshold{Low: 1.2, High: 2.1},
		SDAThreshold: decode.LogicThreshold{Low: 1.2, High: 2.1},
		VDD:          testVDD,
	}
	res, err := decode.DecodeI2C(scl, sda, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 || len(res.Transactions[0].Frames) != 1 {
		t.Fatalf("transactions = %+v", res.Transactions)
	}
	f := res.Transactions[0].Frames[0]
	if f.Address != 0x50 || len(f.Data) != 1 || f.Data[0].Value != 0x3C {
		t.Fatalf("noisy decode = %+v", f)
	}
}

func TestI2CGlitchFilterPreventsFalseSTOP(t *testing.T) {
	cfg := testwave.I2CConfig{
		Wave:        defaultWaveConfig(0, 200e6, 0),
		SCLHighTime: 3e-6,
		SCLLowTime:  6e-6,
		DataHold:    2e-6,
		DataSetup:   4e-6,
		StartHold:   1e-6,
		StartSetup:  2e-6,
		StopSetup:   1.5e-6,
	}
	b := mustI2CBuilder(t, cfg)
	b.Idle(1e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x77, true)
	b.Stop()
	sclT, sdaT := b.Transitions()
	// Inject a 50 ns SDA glitch in the idle-high period after STOP. Without
	// filtering it looks like an extra START/STOP, i.e. an extra transaction.
	stopTime := b.EndTime()
	cfg.Wave.Duration = stopTime - cfg.Wave.StartTime + 3e-6
	sdaT = append(sdaT,
		testwave.Transition{T: stopTime + 1e-6, High: false},
		testwave.Transition{T: stopTime + 1.05e-6, High: true},
	)
	scl, err := testwave.Build(cfg.Wave, true, sclT)
	if err != nil {
		t.Fatal(err)
	}
	sda, err := testwave.Build(cfg.Wave, true, sdaT)
	if err != nil {
		t.Fatal(err)
	}

	opts := baseI2COptions()
	opts.GlitchWidth = 200e-9
	res, err := decode.DecodeI2C(scl, sda, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("glitch created %d transactions, want 1: %+v", len(res.Transactions), res.Diagnostics)
	}
	if !hasDiag(res.Diagnostics, decode.CodeEdgeGlitchSuppressed) {
		t.Fatalf("missing glitch diagnostic: %+v", res.Diagnostics)
	}
}

func TestI2CDifferentSampleRatesAndOffsets(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x11, true)
	b.Stop()
	sclT, sdaT := b.Transitions()
	end := b.EndTime()

	cfgSCL := defaultWaveConfig(-1e-6, 200e6, end+3e-6)
	cfgSDA := defaultWaveConfig(-0.5e-6, 80e6, end+2e-6)
	scl, err := testwave.Build(cfgSCL, true, sclT)
	if err != nil {
		t.Fatal(err)
	}
	sda, err := testwave.Build(cfgSDA, true, sdaT)
	if err != nil {
		t.Fatal(err)
	}
	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d: %+v", len(res.Transactions), res.Diagnostics)
	}
	f := res.Transactions[0].Frames[0]
	if f.Address != 0x50 || len(f.Data) != 1 || f.Data[0].Value != 0x11 {
		t.Fatalf("decode across different rates = %+v", f)
	}
	if !hasDiag(res.Diagnostics, decode.CodeWaveformPartialOverlap) {
		t.Fatalf("expected partial overlap diagnostic: %+v", res.Diagnostics)
	}
}

func TestI2CBadOptionsReturnError(t *testing.T) {
	w := decode.Waveform{Samples: []float32{0, 1}, SampleRate: 1, StartTime: 0}
	bad := []decode.I2COptions{
		{}, // thresholds not configured
		{SCLThreshold: decode.SingleThreshold(1), SDAThreshold: decode.SingleThreshold(1), VDD: -1},
		{SCLThreshold: decode.SingleThreshold(1), SDAThreshold: decode.SingleThreshold(1), GlitchWidth: -1},
		{SCLThreshold: decode.LogicThreshold{Low: 2, High: 1}, SDAThreshold: decode.SingleThreshold(1)},
	}
	for i, o := range bad {
		if _, err := decode.DecodeI2C(w, w, o); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestI2CNoStartCondition(t *testing.T) {
	// Clock-only activity with SDA low and no START must not fabricate a frame.
	cfg := defaultWaveConfig(0, 200e6, 10e-6)
	scl, err := testwave.Build(cfg, false, []testwave.Transition{
		{T: 1e-6, High: true}, {T: 2e-6, High: false},
		{T: 3e-6, High: true}, {T: 4e-6, High: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	sda, err := testwave.Build(cfg, false, nil) // constantly low
	if err != nil {
		t.Fatal(err)
	}
	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 0 {
		t.Fatalf("fabricated transactions: %+v", res.Transactions)
	}
	if !hasDiag(res.Diagnostics, decode.CodeI2CNoStartCondition) {
		t.Fatalf("missing no-start diagnostic: %+v", res.Diagnostics)
	}
}

func TestI2CNoSDAChangeProducesNoFabricatedSetupHold(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true) // address write
	b.Byte(0x00, true) // all-zero data: SDA stays low through the byte
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("transactions = %d", len(res.Transactions))
	}
	// The address ACK and the all-zero byte have no SDA transition between the
	// relevant clock edges, so no setup/hold value may be invented for it.
	for _, tm := range res.Transactions[0].Timings {
		if (tm.Name == decode.TimingSUDAT || tm.Name == decode.TimingHDDAT) && tm.ByteIndex == 1 && tm.Phase != "ack" {
			t.Fatalf("fabricated data setup/hold without an SDA transition: %+v", tm)
		}
	}
	if !hasDiag(res.Transactions[0].Diagnostics, decode.CodeTimingNotObservable) {
		t.Fatalf("missing explanation for unobservable setup/hold: %+v", res.Transactions[0].Diagnostics)
	}
}

func TestI2CResultFlattenedTypedFields(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x10, true)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 1 || len(res.Frames) != 1 {
		t.Fatalf("transactions=%d frames=%d", len(res.Transactions), len(res.Frames))
	}
	if res.Frames[0].TransactionIndex != 0 {
		t.Fatalf("frame transaction index = %d", res.Frames[0].TransactionIndex)
	}
	if len(res.Timings) == 0 {
		t.Fatal("result.Timings is empty")
	}
	for _, tm := range res.Timings {
		if tm.TransactionIndex != 0 {
			t.Fatalf("timing transaction index = %d", tm.TransactionIndex)
		}
		if tm.X2 < tm.X1 || tm.Value != tm.X2-tm.X1 {
			t.Fatalf("invalid typed timing: %+v", tm)
		}
	}
	if len(res.SCLFrequencies) == 0 {
		t.Fatal("result.SCLFrequencies is empty")
	}
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected diagnostics to be exposed on result")
	}
}

func TestI2CMultipleTransactionsFlattening(t *testing.T) {
	b := newTestI2C(t, 0)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x01, true)
	b.Stop()
	b.Idle(5e-6)
	b.Start()
	b.Byte(0xA1, true)
	b.Byte(0x02, false)
	b.Stop()
	scl, sda := buildI2C(t, b)

	res, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 2 || len(res.Frames) != 2 {
		t.Fatalf("transactions=%d frames=%d", len(res.Transactions), len(res.Frames))
	}
	for i, f := range res.Frames {
		if f.TransactionIndex != i {
			t.Fatalf("frame %d transaction index = %d", i, f.TransactionIndex)
		}
	}
	for _, tm := range res.Timings {
		if tm.TransactionIndex < 0 || tm.TransactionIndex >= len(res.Transactions) {
			t.Fatalf("timing transaction index = %d", tm.TransactionIndex)
		}
	}
}
