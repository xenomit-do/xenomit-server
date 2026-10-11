package codec

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMixRoundTrip(t *testing.T) {
	for _, v := range []int32{0, 1, 100, 2000, 20800, -1, 1 << 20} {
		got := Mix(Mix(v, 8, 24), 24, 8)
		if got != v {
			t.Fatalf("rotl 8 of %d came back as %d", v, got)
		}
	}
}

func TestFrameSplitPartial(t *testing.T) {
	w := NewWriter(10996)
	w.WriteInt32(7)
	frame, err := w.Frame()
	if err != nil {
		t.Fatal(err)
	}
	two := append(append([]byte{}, frame...), frame...)
	partial := two[:len(two)-3]
	frames, rest, err := SplitFrames(partial)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || len(rest) == 0 {
		t.Fatalf("frames=%d rest=%d", len(frames), len(rest))
	}
	frames, rest, err = SplitFrames(append(rest, two[len(two)-3:]...))
	if err != nil || len(frames) != 1 || len(rest) != 0 {
		t.Fatalf("reassembly frames=%d rest=%d err=%v", len(frames), len(rest), err)
	}
	if !bytes.Equal(frames[0], frame) {
		t.Fatal("frame mismatch")
	}
}

func TestLengthPrefixBigEndian(t *testing.T) {
	w := NewWriter(29819)
	w.WriteInt16(6455)
	frame, err := w.Frame()
	if err != nil {
		t.Fatal(err)
	}
	n := binary.BigEndian.Uint16(frame[:2])
	if int(n) != len(frame)-2 {
		t.Fatalf("length %d, frame %d", n, len(frame))
	}
	id := binary.BigEndian.Uint16(frame[2:4])
	if int16(id) != 29819 {
		t.Fatalf("id %d", id)
	}
}

func TestRejectHugeFrame(t *testing.T) {
	buf := []byte{0xff, 0xff, 0, 0}
	_, _, err := SplitFrames(buf)
	if err == nil {
		t.Fatal("expected error")
	}
}
