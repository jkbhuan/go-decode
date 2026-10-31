// Command i2c demonstrates decoding an I2C write transaction.
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

	ex, err := demo.BuildI2CExample()
	if err != nil {
		die(err)
	}
	res, err := decode.I2C(ex.SCL, ex.SDA, decode.I2COptions{
		SCLThreshold: decode.SingleThreshold(1.65),
		SDAThreshold: decode.SingleThreshold(1.65),
		VDD:          3.3, // derives 0.3*VDD / 0.7*VDD timing levels
	})
	if err != nil {
		die(err)
	}

	cursor, hasCursor := decode.SelectTiming(res.Timings, decode.TimingFilter{
		Name:      decode.TimingHDSTA,
		Direction: decode.DirectionWrite,
	}, decode.SelectFirst)

	if *asJSON {
		out := struct {
			Result decode.I2CResult
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
		err := waveplot.WritePNG(*pngPath, demo.I2CExampleTitle, []waveplot.Channel{
			{Label: "SCL", Wave: ex.SCL, Color: color.RGBA{31, 119, 180, 255}},
			{Label: "SDA", Wave: ex.SDA, Color: color.RGBA{214, 39, 40, 255}},
		})
		if err != nil {
			die(err)
		}
	}
}

func printResult(res decode.I2CResult, cursor decode.Timing, hasCursor bool) {
	fmt.Printf("I2C transactions=%d frames=%d timings=%d diagnostics=%d\n",
		len(res.Transactions), len(res.Frames), len(res.Timings), len(res.Diagnostics))
	for i, tx := range res.Transactions {
		fmt.Printf("transaction[%d] complete=%v start=%.9gs end=%.9gs\n",
			i, tx.Complete, tx.Start, tx.End)
	}
	for _, f := range res.Frames {
		fmt.Printf("frame tx=%d addr=0x%02X bits=%d dir=%s ack=%s start=%.9gs end=%.9gs\n",
			f.TransactionIndex, f.Address, f.AddressBits, f.Direction, f.AddressACK, f.Start, f.End)
		for _, b := range f.Data {
			fmt.Printf("  byte value=0x%02X ack=%s start=%.9gs end=%.9gs\n",
				b.Value, b.Ack, b.Start, b.End)
		}
	}
	fmt.Println("Timing:")
	for _, r := range res.Timings {
		fmt.Println(r)
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
