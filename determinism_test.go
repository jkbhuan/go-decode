package decode_test

import (
	"reflect"
	"testing"

	"github.com/jkbhuan/go-decode"
)

func TestI2CDecodeIsDeterministic(t *testing.T) {
	b := newTestI2C(t, -1e-6)
	b.Start()
	b.Byte(0xA0, true)
	b.Byte(0x10, true)
	b.RepeatedStart()
	b.Byte(0xA1, true)
	b.Byte(0x5A, false)
	b.Stop()
	scl, sda := buildI2C(t, b)

	a, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := decode.DecodeI2C(scl, sda, baseI2COptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatal("I2C decode is not deterministic")
	}
}

func TestSMIDecodeIsDeterministic(t *testing.T) {
	b := newTestMDIO(t, -1e-6)
	b.Preamble(32)
	b.Frame(decode.DirectionWrite, 0x05, 0x1F, 0xBEEF)
	b.Preamble(32)
	b.Frame(decode.DirectionRead, 0x06, 0x02, 0x1234)
	mdc, mdio := buildMDIO(t, b)

	a, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := decode.DecodeSMI(mdc, mdio, baseSMIOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatal("SMI decode is not deterministic")
	}
}
