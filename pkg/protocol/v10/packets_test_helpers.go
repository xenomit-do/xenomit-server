package v10

import "github.com/xenomit/server/pkg/protocol/codec"

func readRaw(frame []byte) (*codec.Reader, error) {
	return codec.NewReader(frame)
}
