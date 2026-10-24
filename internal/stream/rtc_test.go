package stream

import (
	"bytes"
	"testing"
)

// ffmpeg stopped before its first byte (the window closed or moved as it started)
func TestPumpH264Empty(t *testing.T) {
	for _, in := range [][]byte{nil, {0}} {
		if err := pumpH264(bytes.NewReader(in), func([]byte) error { return nil }); err != nil {
			t.Errorf("%v: %v", in, err)
		}
	}
}
