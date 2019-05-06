package main

import "encoding/binary"

func times(str string, n int) (out string) {
	for i := 0; i < n; i++ {
		out += str
	}
	return
}

func padRight(str string, length int, pad string) string {
	return str + times(pad, length-len(str))
}

func makeUint16Slice(buf []byte) []uint16 {
	ret := make([]uint16, len(buf)/2)

	for i := 0; i < len(buf); i += 2 {
		ret[i/2] = binary.BigEndian.Uint16(buf[i : i+2])
	}

	return ret
}
