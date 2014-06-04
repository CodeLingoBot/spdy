package frames

import (
	"io"

	"github.com/SlyMarbo/spdy/common"
)

type NoopFrame struct{}

func (frame *NoopFrame) Compress(comp common.Compressor) error {
	return nil
}

func (frame *NoopFrame) Decompress(decomp common.Decompressor) error {
	return nil
}

func (frame *NoopFrame) Name() string {
	return "NOOP"
}

func (frame *NoopFrame) ReadFrom(reader io.Reader) (int64, error) {
	data, err := common.ReadExactly(reader, 8)
	if err != nil {
		return 0, err
	}

	err = controlFrameCommonProcessing(data[:5], NOOP, 0)
	if err != nil {
		return 8, err
	}

	// Get and check length.
	length := int(common.BytesToUint24(data[5:8]))
	if length != 0 {
		return 8, common.IncorrectDataLength(length, 0)
	}

	return 8, nil
}

func (frame *NoopFrame) String() string {
	return "NOOP {\n\tVersion:              2\n}\n"
}

func (frame *NoopFrame) WriteTo(writer io.Writer) (int64, error) {
	return 0, nil
}