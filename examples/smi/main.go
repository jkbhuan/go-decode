// Command smi demonstrates decoding an IEEE 802.3 Clause 22 MDIO write frame.
//
// By default it prints a typed Go summary built from result.Frames,
// result.Timings and result.Diagnostics. Pass -json to serialize the same
// structures with encoding/json (JSON is an upper-layer concern, not part of
// the decode package). Pass -png to render the synthesized waveform.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image/color"
	"os"

	"github.com/jkbhuan/go-decode"
	"github.com/jkbhuan/go-decode/examples/internal/demo"
	"github.com/jkbhuan/go-decode/internal/waveplot"
)

func main() {
	pngPath := flag.String("png", "", "write the demo waveform to this PNG path")
	asJSON := flag.Bool("json", false, "print the typed result as JSON (upper-layer serialization)")
	flag.Parse()

	ex, err := demo.BuildSMIExample()
	if err != nil {
		die(err)
	}
	res, err := decode.SMI(ex.MDC, ex.MDIO, decode.SMIOptions{
		MDCThreshold:    decode.SingleThreshold(1.65),
		MDIOThreshold:   decode.SingleThreshold(1.65),
		TimingThreshold: &decode.LogicThreshold{Low: 0.8, High: 2.0}, // MII V_IL/V_IH
	})
	if err != nil {
		die(err)
	}

	cursor, hasCursor := decode.SelectTiming(res.Timings, decode.TimingFilter{
		Name:  decode.TimingSUMDIO,
		Phase: "data",
	}, decode.SelectMin)

	if *asJSON {
		out := struct {
			Result decode.SMIResult
			Cursor *decode.Timing
		}{Result: res}
		if hasCursor {
			out.Cursor = &cursor
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			die(err)
		}
	} else {
		printResult(res, cursor, hasCursor)
	}

	if *pngPath != "" {
		err := waveplot.WritePNG(*pngPath, demo.SMIExampleTitle, []waveplot.Channel{
			{Label: "MDC", Wave: ex.MDC, Color: color.RGBA{31, 119, 180, 255}},
			{Label: "MDIO", Wave: ex.MDIO, Color: color.RGBA{44, 160, 44, 255}},
		})
		if err != nil {
			die(err)
		}
	}
}

func printResult(res decode.SMIResult, cursor decode.Timing, hasCursor bool) {
	fmt.Printf("SMI frames=%d timings=%d diagnostics=%d\n",
		len(res.Frames), len(res.Timings), len(res.Diagnostics))
	for i, f := range res.Frames {
		fmt.Printf("frame[%d] dir=%s phy=0x%02X reg=0x%02X data=0x%04X op=0x%02X preamble=%d ta=%s complete=%v dataValid=%v bits=%d\n",
			i, f.Direction, f.PHYAddr, f.RegAddr, f.Data, f.OpCode, f.PreambleBits, f.Turnaround,
			f.Complete, f.DataValid, f.BitsReceived)
		for _, d := range f.Diagnostics {
			fmt.Printf("  frame diagnostic [%s] %s: %s\n", d.Severity, d.Code, d.Message)
		}
	}
	if hasCursor {
		fmt.Printf("cursor %s value=%.9gs X1=%.9gs X2=%.9gs phase=%s dir=%s\n",
			cursor.Name, cursor.Value, cursor.X1, cursor.X2, cursor.Phase, cursor.Direction)
	}
	for _, d := range res.Diagnostics {
		fmt.Printf("diagnostic [%s] %s: %s\n", d.Severity, d.Code, d.Message)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
