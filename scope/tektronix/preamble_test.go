package tektronix_test

import (
	"testing"

	"github.com/jkbhuan/go-decode/scope/tektronix"
)

func TestParseWFMOutpre(t *testing.T) {
	const text = "BYT_NR 2;BIT_NR 16;ENCDG BINARY;BN_FMT RI;BYT_OR LSB;NR_PT 4;PT_FMT Y;" +
		"XINCR 1e-9;XZERO -2e-9;PT_OFF 2;YMULT 0.1;YOFF 0;YZERO 0;XUNIT \"s\";YUNIT \"V\""
	p, err := tektronix.ParseWFMOutpre(text)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := p.EncodingSpec()
	if err != nil {
		t.Fatal(err)
	}
	if p.NumPoints != 4 || p.XIncrement != 1e-9 || p.PTOffset != 2 || p.YMult != 0.1 {
		t.Fatalf("preamble = %+v", p)
	}
	if enc.BytesPerPoint != 2 || !enc.Signed || enc.ByteOrder != "little" {
		t.Fatalf("encoding = %+v", enc)
	}
	if _, err := tektronix.ParseWFMOutpre("BYT_NR 1;ENCDG ASCII"); err == nil {
		t.Fatal("ASCII should be rejected")
	}
}
