package hub

import (
	"fmt"
	"strconv"
	"strings"
)

// cksum computes the POSIX `cksum` CRC, which every Unix — AIX included —
// can recompute on the host to prove an archive arrived intact.
type cksum struct {
	crc uint32
	n   uint64
}

var crcTable = func() (t [256]uint32) {
	for i := range t {
		c := uint32(i) << 24
		for range 8 {
			if c&0x80000000 != 0 {
				c = c<<1 ^ 0x04C11DB7
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return t
}()

func (c *cksum) Write(p []byte) (int, error) {
	for _, b := range p {
		c.crc = c.crc<<8 ^ crcTable[byte(c.crc>>24)^b]
	}
	c.n += uint64(len(p))
	return len(p), nil
}

// Sum is "<crc> <size>", the first two fields of `cksum file`.
func (c *cksum) Sum() string {
	crc := c.crc
	for n := c.n; n > 0; n >>= 8 {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^byte(n)]
	}
	return fmt.Sprintf("%d %d", ^crc, c.n)
}

// parseCksumLine returns "<crc> <size>" from one `cksum` output line.
func parseCksumLine(line string) (string, bool) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return "", false
	}
	if _, err := strconv.ParseUint(f[0], 10, 32); err != nil {
		return "", false
	}
	return f[0] + " " + f[1], true
}
