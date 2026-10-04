package main

import (
	"io"
	"strconv"
)

type output struct {
	dst io.Writer
	buf []byte
	err error
}

func newOutput(dst io.Writer) *output {
	return &output{dst: dst, buf: make([]byte, 0, 512)}
}

func (o *output) add(parts ...string) *output {
	for _, part := range parts {
		o.buf = append(o.buf, part...)
	}
	return o
}

func (o *output) num(n int64) *output {
	o.buf = strconv.AppendInt(o.buf, n, 10)
	return o
}

func (o *output) twoDigits(n int64) *output {
	if n < 10 {
		o.buf = append(o.buf, '0')
	}
	return o.num(n)
}

func (o *output) cell(text string, width int) *output {
	o.buf = append(o.buf, text...)
	for i := len(text); i < width; i++ {
		o.buf = append(o.buf, ' ')
	}
	return o
}

func (o *output) end() {
	o.buf = append(o.buf, '\n')
	if o.err == nil {
		_, o.err = o.dst.Write(o.buf)
	}
	o.buf = o.buf[:0]
}

func printable(untrusted string) string {
	for i := 0; i < len(untrusted); i++ {
		if untrusted[i] < 0x20 || untrusted[i] == 0x7f {
			clean := []byte(untrusted)
			for j := i; j < len(clean); j++ {
				if clean[j] < 0x20 || clean[j] == 0x7f {
					clean[j] = '?'
				}
			}
			return string(clean)
		}
	}
	return untrusted
}
