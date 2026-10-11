// Package codec is the DarkOrbit 10.0 byte buffer: big-endian integers,
// a 2-byte length prefix, and the per-field bit rotations the client applies.
package codec

import (
	"encoding/binary"
	"errors"
	"math"
)

// MaxFrame is the largest accepted payload (command id + body), in bytes.
// The original emulator treated every 1 KB read as one packet.
const MaxFrame = 32768

var (
	ErrShort = errors.New("codec: short buffer")
	ErrFrame = errors.New("codec: invalid frame")
)

// Writer builds a command. The first field is the command id.
// Inner is the id plus payload (what nested modules embed).
// Frame prepends the 2-byte big-endian length of Inner.
type Writer struct {
	buf []byte
}

func NewWriter(id int16) *Writer {
	w := &Writer{}
	w.WriteInt16(id)
	return w
}

func (w *Writer) WriteInt16(v int16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(v))
	w.buf = append(w.buf, b[:]...)
}

func (w *Writer) WriteInt32(v int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	w.buf = append(w.buf, b[:]...)
}

func (w *Writer) WriteFloat32(v float32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], math.Float32bits(v))
	w.buf = append(w.buf, b[:]...)
}

func (w *Writer) WriteFloat64(v float64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	w.buf = append(w.buf, b[:]...)
}

func (w *Writer) WriteBool(v bool) {
	if v {
		w.buf = append(w.buf, 1)
		return
	}
	w.buf = append(w.buf, 0)
}

func (w *Writer) WriteUTF(s string) {
	b := []byte(s)
	if len(b) > 32767 {
		b = b[:32767]
	}
	w.WriteInt16(int16(len(b)))
	w.buf = append(w.buf, b...)
}

// Raw appends an already-encoded nested module (id + payload, no length).
func (w *Writer) Raw(b []byte) { w.buf = append(w.buf, b...) }

func (w *Writer) Inner() []byte {
	out := make([]byte, len(w.buf))
	copy(out, w.buf)
	return out
}

func (w *Writer) Frame() ([]byte, error) {
	if len(w.buf) < 2 || len(w.buf) > MaxFrame {
		return nil, ErrFrame
	}
	out := make([]byte, 2+len(w.buf))
	binary.BigEndian.PutUint16(out[:2], uint16(len(w.buf)))
	copy(out[2:], w.buf)
	return out, nil
}

// Reader walks a framed packet. It consumes the length and command id,
// matching ByteParser in the DarkOrbit 10.0 emulator.
type Reader struct {
	b  []byte
	i  int
	ID int16
}

func NewReader(frame []byte) (*Reader, error) {
	if len(frame) < 4 {
		return nil, ErrShort
	}
	n := int(binary.BigEndian.Uint16(frame[:2]))
	if n < 2 || n > MaxFrame || len(frame) < 2+n {
		return nil, ErrFrame
	}
	r := &Reader{b: frame[:2+n]}
	if _, err := r.ReadInt16(); err != nil {
		return nil, err
	}
	id, err := r.ReadInt16()
	if err != nil {
		return nil, err
	}
	r.ID = id
	return r, nil
}

func (r *Reader) ReadByte() (byte, error) {
	if r.i >= len(r.b) {
		return 0, ErrShort
	}
	v := r.b[r.i]
	r.i++
	return v, nil
}

func (r *Reader) ReadInt16() (int16, error) {
	if r.i+2 > len(r.b) {
		return 0, ErrShort
	}
	v := int16(binary.BigEndian.Uint16(r.b[r.i:]))
	r.i += 2
	return v, nil
}

func (r *Reader) ReadInt32() (int32, error) {
	if r.i+4 > len(r.b) {
		return 0, ErrShort
	}
	v := int32(binary.BigEndian.Uint32(r.b[r.i:]))
	r.i += 4
	return v, nil
}

func (r *Reader) ReadBool() (bool, error) {
	b, err := r.ReadByte()
	if err != nil {
		return false, err
	}
	return b == 1, nil
}

func (r *Reader) ReadUTF() (string, error) {
	n, err := r.ReadInt16()
	if err != nil {
		return "", err
	}
	if n < 0 || r.i+int(n) > len(r.b) {
		return "", ErrFrame
	}
	s := string(r.b[r.i : r.i+int(n)])
	r.i += int(n)
	return s, nil
}

// Mix is the DarkOrbit field obfuscation (v << left) | (v >> right)
// on the uint32 bit pattern. Left + right is 32 for every catalogued field,
// which makes Mix a rotation.
func Mix(v int32, left, right uint) int32 {
	u := uint32(v)
	return int32((u << left) | (u >> right))
}

// SplitFrames pulls every complete frame out of buf and returns the unparsed tail.
func SplitFrames(buf []byte) (frames [][]byte, rest []byte, err error) {
	for {
		if len(buf) < 2 {
			return frames, buf, nil
		}
		n := int(binary.BigEndian.Uint16(buf[:2]))
		if n < 2 || n > MaxFrame {
			return frames, buf, ErrFrame
		}
		if len(buf) < 2+n {
			return frames, buf, nil
		}
		frame := make([]byte, 2+n)
		copy(frame, buf[:2+n])
		frames = append(frames, frame)
		buf = buf[2+n:]
	}
}
