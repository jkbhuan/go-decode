package scope

import (
	"errors"
	"fmt"
)

// ErrInvalidBlock is returned when an IEEE 488.2 definite-length block is
// malformed.
var ErrInvalidBlock = errors.New("decode: invalid IEEE 488.2 definite-length block")

// ParseIEEE4882DefiniteBlock parses an IEEE 488.2 definite-length binary block
// header and returns its payload and the bytes following the block.
//
// Example: "#42000<binary>" -> payload of 2000 bytes, rest = remaining bytes.
// "#0" (indefinite length, terminated by EOI) cannot be represented in a byte
// slice and is rejected.
func ParseIEEE4882DefiniteBlock(data []byte) (payload, rest []byte, err error) {
	i := 0
	for i < len(data) && (data[i] == ' ' || data[i] == '\r' || data[i] == '\n' || data[i] == '\t') {
		i++
	}
	if i >= len(data) || data[i] != '#' {
		return nil, nil, fmt.Errorf("%w: missing '#' header", ErrInvalidBlock)
	}
	i++
	if i >= len(data) {
		return nil, nil, fmt.Errorf("%w: truncated header", ErrInvalidBlock)
	}
	nd := data[i]
	i++
	if nd == '0' {
		return nil, nil, fmt.Errorf("%w: indefinite-length (#0) blocks are not supported", ErrInvalidBlock)
	}
	if nd < '1' || nd > '9' {
		return nil, nil, fmt.Errorf("%w: invalid digit count %q", ErrInvalidBlock, nd)
	}
	nDigits := int(nd - '0')
	if i+nDigits > len(data) {
		return nil, nil, fmt.Errorf("%w: truncated length field", ErrInvalidBlock)
	}
	var length int
	for _, c := range data[i : i+nDigits] {
		if c < '0' || c > '9' {
			return nil, nil, fmt.Errorf("%w: invalid length digit %q", ErrInvalidBlock, c)
		}
		length = length*10 + int(c-'0')
	}
	i += nDigits
	if length > len(data)-i {
		return nil, nil, fmt.Errorf("%w: declared length %d exceeds remaining %d bytes", ErrInvalidBlock, length, len(data)-i)
	}
	return data[i : i+length], data[i+length:], nil
}
