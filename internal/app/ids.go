package app

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// SystemClock returns UTC now.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// UUIDGenerator issues RFC 9562 UUID v7 strings (matches init.md examples).
// Implemented in-process so the application layer does not import database/sql
// (which github.com/google/uuid pulls in via driver.Valuer).
type UUIDGenerator struct{}

func (UUIDGenerator) NewID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli()) //nolint:gosec // UUID v7 timestamp width is 48 bits
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	_, _ = rand.Read(b[6:])
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
