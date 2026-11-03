package decode_test

import (
	"testing"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/internal/testwave"
)

func newTestMDIO(t *testing.T, start float64) *testwave.MDIOBuilder {
	t.Helper()
	return mustMDIOBuilder(t, testwave.MDIOConfig{
		Wave:        defaultWaveConfig(start, 200e6, 0),
		MDCPeriod:   1e-6,
		MDCHighTime: 0.5e-6,
		DataHold:    0.2e-6,
		DataSetup:   0.3e-6,
	})
}

func TestSMIWriteFrame(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x05, 0x1F, 0xBEEF)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d: %+v", len(res.Frames), res.Diagnostics)
	}
	f := res.Frames[0]
	if f.Direction != decode.DirectionWrite {
		t.Fatalf("direction = %s", f.Direction)
	}
	if f.PHYAddr != 0x05 || f.RegAddr != 0x1F || f.Data != 0xBEEF {
		t.Fatalf("fields: phy=%#x reg=%#x data=%#x", f.PHYAddr, f.RegAddr, f.Data)
	}
	if f.PreambleBits != 32 {
		t.Fatalf("preamble bits = %d", f.PreambleBits)
	}
	if f.Turnaround != decode.StatusValid {
		t.Fatalf("turnaround = %s", f.Turnaround)
	}
	if !f.Complete || !f.DataValid || f.BitsReceived != 32 {
		t.Fatalf("frame flags: %+v", f)
	}

	tol := 2.0 / 200e6
	highs := findTiming(f.Timings, decode.TimingMDCHigh)
	if len(highs) == 0 {
		t.Fatalf("no MDC high timings: %+v", f.Diagnostics)
	}
	for _, tm := range highs {
		approx(t, tm.Value, 0.5e-6, tol, "MDCHigh")
	}
	lows := findTiming(f.Timings, decode.TimingMDCLow)
	if len(lows) == 0 {
		t.Fatalf("no MDC low timings")
	}
	for _, tm := range lows {
		approx(t, tm.Value, 0.5e-6, tol, "MDCLow")
	}
	periods := findTiming(f.Timings, decode.TimingMDCPeriod)
	if len(periods) == 0 {
		t.Fatalf("no MDC periods")
	}
	for _, tm := range periods {
		approx(t, tm.Value, 1e-6, tol, "MDCPeriod")
	}

	sus := findTiming(f.Timings, decode.TimingSUMDIO)
	if len(sus) == 0 {
		t.Fatalf("no tSU(MDIO) candidates: %+v", f.Diagnostics)
	}
	for _, tm := range sus {
		approx(t, tm.Value, 0.3e-6, tol, "tSU(MDIO)")
		checkTimingInvariant(t, tm)
	}
	hds := findTiming(f.Timings, decode.TimingHDMDIO)
	if len(hds) == 0 {
		t.Fatalf("no tHD(MDIO) candidates")
	}
	for _, tm := range hds {
		approx(t, tm.Value, 0.7e-6, tol, "tHD(MDIO)")
		checkTimingInvariant(t, tm)
	}
	// The DATA phase must be distinguishable from the header phases.
	dataPhase := 0
	for _, tm := range sus {
		if tm.Phase == "data" {
			dataPhase++
		}
	}
	if dataPhase == 0 {
		t.Fatal("no DATA-phase setup candidates")
	}
	for _, tm := range f.Timings {
		checkTimingInvariant(t, tm)
	}
}

func TestSMIReadFrameTAUnverifiable(t *testing.T) {
	b := newTestMDIO(t, -2e-6)
	b.Preamble(32)
	b.Frame(decode.DirectionRead, 0x1E, 0x02, 0x1234)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d", len(res.Frames))
	}
	f := res.Frames[0]
	if f.Direction != decode.DirectionRead {
		t.Fatalf("direction = %s", f.Direction)
	}
	if f.Turnaround != decode.StatusUnverifiable {
		t.Fatalf("read TA must be unverifiable, got %s", f.Turnaround)
	}
	if !hasDiag(f.Diagnostics, decode.CodeSMIReadTAUnverifiable) {
		t.Fatalf("missing high-Z diagnostic: %+v", f.Diagnostics)
	}
	if f.PHYAddr != 0x1E || f.RegAddr != 0x02 || f.Data != 0x1234 {
		t.Fatalf("fields: %+v", f)
	}
	// Read-direction tSU/tHD use the same names as write, distinguished by
	// Direction. This waveform uses the STA convention (MDIO changes at
	// fall+DataHold), so the values match the write definitions: the preceding
	// rising edge to the launch gives tSU, and the launch of the next bit
	// gives tHD.
	sus := findTiming(f.Timings, decode.TimingSUMDIO)
	if len(sus) == 0 {
		t.Fatalf("no read tSU(MDIO) candidates: %+v", f.Diagnostics)
	}
	for _, tm := range sus {
		if tm.Direction != decode.DirectionRead {
			t.Fatalf("tSU direction = %s, want read", tm.Direction)
		}
		approx(t, tm.Value, 0.3e-6, 2.0/200e6, "tSU(MDIO)")
		checkTimingInvariant(t, tm)
	}
	hds := findTiming(f.Timings, decode.TimingHDMDIO)
	if len(hds) == 0 {
		t.Fatalf("no read tHD(MDIO) candidates: %+v", f.Diagnostics)
	}
	for _, tm := range hds {
		if tm.Direction != decode.DirectionRead {
			t.Fatalf("tHD direction = %s, want read", tm.Direction)
		}
		approx(t, tm.Value, 0.7e-6, 2.0/200e6, "tHD(MDIO)")
		checkTimingInvariant(t, tm)
	}
	if hasDiag(f.Diagnostics, decode.CodeSMIReadTimingUnsupported) {
		t.Fatalf("deprecated read-timing diagnostic must not be emitted: %+v", f.Diagnostics)
	}
	if len(findTiming(f.Timings, decode.TimingMDCPeriod)) == 0 {
		t.Fatal("MDC period should still be measured for read frames")
	}
}

func TestSMIReadSetupHoldPHYDriven(t *testing.T) {
	const (
		tco    = 20e-9
		period = 1e-6
	)
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	if err := b.ReadFrame(0x05, 0x1F, 0xBEEF, tco); err != nil {
		t.Fatal(err)
	}
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d: %+v", len(res.Frames), res.Diagnostics)
	}
	f := res.Frames[0]
	if f.Direction != decode.DirectionRead || f.Data != 0xBEEF {
		t.Fatalf("frame = %+v", f)
	}

	sawTurnaround, sawData := false, false
	sus := findTiming(f.Timings, decode.TimingSUMDIO)
	if len(sus) == 0 {
		t.Fatalf("no read tSU(MDIO) candidates: %+v", f.Diagnostics)
	}
	for _, tm := range sus {
		if tm.Direction != decode.DirectionRead {
			t.Fatalf("tSU direction = %s, want read", tm.Direction)
		}
		approx(t, tm.Value, period-tco, 2.0/200e6, "tSU(MDIO)")
		checkTimingInvariant(t, tm)
		switch tm.Phase {
		case "turnaround":
			sawTurnaround = true
		case "data":
			sawData = true
		}
	}
	if !sawTurnaround {
		t.Fatal("no read tSU candidate on the PHY-driven second turnaround bit")
	}
	if !sawData {
		t.Fatal("no read tSU candidate in the data phase")
	}

	hds := findTiming(f.Timings, decode.TimingHDMDIO)
	if len(hds) == 0 {
		t.Fatalf("no read tHD(MDIO) candidates: %+v", f.Diagnostics)
	}
	for _, tm := range hds {
		if tm.Direction != decode.DirectionRead {
			t.Fatalf("tHD direction = %s, want read", tm.Direction)
		}
		approx(t, tm.Value, tco, 2.0/200e6, "tHD(MDIO)")
		checkTimingInvariant(t, tm)
	}
}

func TestSMIPreambleSuppression(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Frame(decode.DirectionWrite, 0x00, 0x00, 0xAAAA)
	mdc, mdio := buildMDIO(t, b)

	opts := baseSMIOptions()
	res, err := decode.DecodeSMI(mdc, mdio, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 0 {
		t.Fatalf("frame decoded without preamble while suppression is disabled: %+v", res.Frames)
	}

	opts.AllowPreambleSuppression = true
	res, err = decode.DecodeSMI(mdc, mdio, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames with suppression = %d: %+v", len(res.Frames), res.Diagnostics)
	}
	if res.Frames[0].PreambleBits != 0 {
		t.Fatalf("preamble bits = %d, want 0", res.Frames[0].PreambleBits)
	}
	if res.Frames[0].Data != 0xAAAA {
		t.Fatalf("data = %#x", res.Frames[0].Data)
	}
	if hasDiag(res.Diagnostics, decode.CodeSMIInvalidST) {
		t.Fatalf("trailing idle cycles were mistaken for a frame: %+v", res.Diagnostics)
	}
}

func TestSMIDataPattern01DoesNotStartFrame(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x02, 0x03, 0x5555) // contains many 01 patterns
	mdc, mdio := buildMDIO(t, b)

	opts := baseSMIOptions()
	opts.AllowPreambleSuppression = true
	res, err := decode.DecodeSMI(mdc, mdio, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("data 01 pattern produced %d frames, want 1: %+v", len(res.Frames), res.Diagnostics)
	}
	if res.Frames[0].PreambleBits != 32 {
		t.Fatalf("preamble bits = %d", res.Frames[0].PreambleBits)
	}
}

func TestSMIMultipleConsecutiveFrames(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x01, 0x00, 0x1111)
	b.Preamble(32)
	b.Frame(decode.DirectionRead, 0x02, 0x01, 0x2222)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 2 {
		t.Fatalf("frames = %d, want 2: %+v", len(res.Frames), res.Diagnostics)
	}
	if res.Frames[0].Direction != decode.DirectionWrite || res.Frames[0].Data != 0x1111 {
		t.Fatalf("frame 0 = %+v", res.Frames[0])
	}
	if res.Frames[1].Direction != decode.DirectionRead || res.Frames[1].Data != 0x2222 {
		t.Fatalf("frame 1 = %+v", res.Frames[1])
	}
	// The last write bit must not borrow an MDIO transition from the next
	// frame's preamble; every hold candidate inside the frame is < 1 period.
	for _, tm := range findTiming(res.Frames[0].Timings, decode.TimingHDMDIO) {
		if tm.Value > 1e-6 {
			t.Fatalf("tHD(MDIO) borrowed an inter-frame transition: %+v", tm)
		}
	}
}

func TestSMIInvalidOpcode(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	bits := []bool{false, true}       // ST
	bits = append(bits, false, false) // OP = 00
	bits = append(bits, testwave.BitsMSB(0x03, 5)...)
	bits = append(bits, testwave.BitsMSB(0x04, 5)...)
	bits = append(bits, true, false) // TA 10
	bits = append(bits, testwave.BitsMSB(0xCAFE, 16)...)
	b.Bits(bits)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d: %+v", len(res.Frames), res.Diagnostics)
	}
	f := res.Frames[0]
	if f.Direction != decode.DirectionUnknown || f.OpCode != 0 {
		t.Fatalf("invalid opcode frame = %+v", f)
	}
	if f.PHYAddr != 0x03 || f.RegAddr != 0x04 || f.Data != 0xCAFE {
		t.Fatalf("observed fields lost: %+v", f)
	}
	if !f.Complete || !f.DataValid {
		t.Fatalf("observed frame flags: %+v", f)
	}
	if !hasDiag(f.Diagnostics, decode.CodeSMIInvalidOpcode) {
		t.Fatalf("missing opcode diagnostic: %+v", f.Diagnostics)
	}
}

func TestSMIInvalidTurnaround(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	bits := []bool{false, true}      // ST
	bits = append(bits, false, true) // OP write
	bits = append(bits, testwave.BitsMSB(0x01, 5)...)
	bits = append(bits, testwave.BitsMSB(0x02, 5)...)
	bits = append(bits, false, false) // TA 00 (illegal)
	bits = append(bits, testwave.BitsMSB(0x1234, 16)...)
	b.Bits(bits)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	f := res.Frames[0]
	if f.Turnaround != decode.StatusInvalid {
		t.Fatalf("turnaround = %s", f.Turnaround)
	}
	if !hasDiag(f.Diagnostics, decode.CodeSMIInvalidTurnaround) {
		t.Fatalf("missing TA diagnostic: %+v", f.Diagnostics)
	}
}

func TestSMIInvalidStartOfFrame(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Bits([]bool{false, false}) // ST candidate 00
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 0 {
		t.Fatalf("invalid ST produced a frame: %+v", res.Frames)
	}
	if !hasDiag(res.Diagnostics, decode.CodeSMIInvalidST) {
		t.Fatalf("missing invalid-ST diagnostic: %+v", res.Diagnostics)
	}
}

func TestSMITruncatedFrame(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	bits := []bool{false, true} // ST
	bits = append(bits, true, false)
	bits = append(bits, testwave.BitsMSB(0x1A, 5)...)
	bits = append(bits, testwave.BitsMSB(0x0B, 5)...)
	bits = append(bits, true, false)                  // TA
	bits = append(bits, testwave.BitsMSB(0xAB, 8)...) // only 8 of 16 data bits
	b.Bits(bits)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d: %+v", len(res.Frames), res.Diagnostics)
	}
	f := res.Frames[0]
	if f.Complete || f.DataValid {
		t.Fatalf("truncated frame marked complete/data-valid: %+v", f)
	}
	if f.BitsReceived >= 32 {
		t.Fatalf("bits received = %d", f.BitsReceived)
	}
	if f.PHYAddr != 0x1A || f.RegAddr != 0x0B {
		t.Fatalf("fully received header not preserved: %+v", f)
	}
	if !hasDiag(f.Diagnostics, decode.CodeSMITruncatedFrame) {
		t.Fatalf("missing truncation diagnostic: %+v", f.Diagnostics)
	}
}

func TestSMIWithoutTimingThreshold(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x01, 0x02, 0x0001)
	mdc, mdio := buildMDIO(t, b)

	opts := baseSMIOptions()
	opts.TimingThreshold = nil
	res, err := decode.DecodeSMI(mdc, mdio, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d", len(res.Frames))
	}
	if len(res.Frames[0].Timings) != 0 {
		t.Fatal("timings must not be produced without a timing threshold")
	}
	if !hasDiag(res.Diagnostics, decode.CodeTimingThresholdMissing) {
		t.Fatalf("missing threshold diagnostic: %+v", res.Diagnostics)
	}
}

func TestSMIDifferentSampleRatesAndOffsets(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x0A, 0x0B, 0x1357)
	mdcT, mdioT := b.Transitions()
	end := b.EndTime()

	cfgMDC := defaultWaveConfig(-1e-6, 200e6, end+2e-6)
	cfgMDIO := defaultWaveConfig(-0.5e-6, 80e6, end+1e-6)
	mdc, err := testwave.Build(cfgMDC, true, mdcT)
	if err != nil {
		t.Fatal(err)
	}
	mdio, err := testwave.Build(cfgMDIO, true, mdioT)
	if err != nil {
		t.Fatal(err)
	}
	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames = %d: %+v", len(res.Frames), res.Diagnostics)
	}
	f := res.Frames[0]
	if f.PHYAddr != 0x0A || f.RegAddr != 0x0B || f.Data != 0x1357 {
		t.Fatalf("fields across rates: %+v", f)
	}
	if !hasDiag(res.Diagnostics, decode.CodeWaveformPartialOverlap) {
		t.Fatalf("missing partial-overlap diagnostic: %+v", res.Diagnostics)
	}
}

func TestSMIBadOptionsReturnError(t *testing.T) {
	w := decode.Waveform{Samples: []float32{0, 1}, SampleRate: 1, StartTime: 0}
	bad := []decode.SMIOptions{
		{},
		{MDCThreshold: decode.SingleThreshold(1), MDIOThreshold: decode.SingleThreshold(1), GlitchWidth: -1},
		{MDCThreshold: decode.SingleThreshold(1), MDIOThreshold: decode.LogicThreshold{Low: 2, High: 1}},
		{MDCThreshold: decode.SingleThreshold(1), MDIOThreshold: decode.SingleThreshold(1), MinPreambleBits: -1},
	}
	for i, o := range bad {
		if _, err := decode.DecodeSMI(w, w, o); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestSMIShortPreambleIgnoredWhenSuppressionDisabled(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(8)
	b.Frame(decode.DirectionWrite, 0x01, 0x02, 0x0003)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 0 {
		t.Fatalf("short preamble accepted: %+v", res.Frames)
	}
	if !hasDiag(res.Diagnostics, decode.CodeSMIShortPreamble) {
		t.Fatalf("missing short-preamble diagnostic: %+v", res.Diagnostics)
	}
}

func TestSMIMidCaptureStartDoesNotFabricateFrame(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x03, 0x04, 0x0000)
	mdc, mdio := buildMDIO(t, b)

	// Start the record in the middle of the preamble: only 21 preamble ones
	// remain. With suppression disabled there is no valid 32-one PRE, so no
	// frame may be invented.
	crop := 10.5 * 1e-6
	mdc = cropWaveform(t, mdc, crop)
	mdio = cropWaveform(t, mdio, crop)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 0 {
		t.Fatalf("mid-capture start fabricated frame(s): %+v", res.Frames)
	}
	if !hasDiag(res.Diagnostics, decode.CodeSMINoFrames) {
		t.Fatalf("missing no-frames diagnostic: %+v", res.Diagnostics)
	}
}

func cropWaveform(t *testing.T, w decode.Waveform, at float64) decode.Waveform {
	t.Helper()
	idx := 0
	for idx < len(w.Samples) {
		if tm, _ := w.TimeAt(idx); tm >= at {
			break
		}
		idx++
	}
	if idx >= len(w.Samples) {
		t.Fatalf("crop time %g beyond record", at)
	}
	out := decode.Waveform{Samples: w.Samples[idx:], SampleRate: w.SampleRate, StartTime: w.StartTime + float64(idx)/w.SampleRate}
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSMIResultFlattenedTypedFields(t *testing.T) {
	b := newTestMDIO(t, 0)
	b.Preamble(32)
	b.Frame(decode.DirectionRead, 0x05, 0x1F, 0xBEEF)
	mdc, mdio := buildMDIO(t, b)

	res, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("frames=%d", len(res.Frames))
	}
	if len(res.Timings) == 0 {
		t.Fatal("result.Timings is empty")
	}
	for _, tm := range res.Timings {
		if tm.FrameIndex != 0 {
			t.Fatalf("timing frame index = %d", tm.FrameIndex)
		}
		if tm.TransactionIndex != -1 {
			t.Fatalf("SMI timing transaction index = %d", tm.TransactionIndex)
		}
		if tm.X2 < tm.X1 || tm.Value != tm.X2-tm.X1 {
			t.Fatalf("invalid typed timing: %+v", tm)
		}
	}
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected diagnostics to be exposed on result")
	}
}
