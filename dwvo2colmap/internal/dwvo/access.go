package dwvo

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"dwvo2colmap/internal/model"
)

func ReadBlockAt(filePath string, header model.Header, offset int64) (model.TimestampBlock, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return model.TimestampBlock{}, err
	}
	defer file.Close()

	return ReadBlockAtFromFile(file, header, offset)
}

func ReadBlockAtFromFile(file *os.File, header model.Header, offset int64) (model.TimestampBlock, error) {
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return model.TimestampBlock{}, err
	}

	var block model.TimestampBlock
	if err := binary.Read(file, binary.LittleEndian, &block.Timestamp); err != nil {
		return model.TimestampBlock{}, fmt.Errorf("read timestamp: %w", err)
	}

	block.VideoFrames = make([]model.VideoFrame, int(header.NCameras))
	for i := range block.VideoFrames {
		busID, err := readNullTerminatedString(file)
		if err != nil {
			return model.TimestampBlock{}, fmt.Errorf("read bus ID for frame %d: %w", i, err)
		}
		block.VideoFrames[i].BusID = busID

		if err := binary.Read(file, binary.LittleEndian, &block.VideoFrames[i].Length); err != nil {
			return model.TimestampBlock{}, fmt.Errorf("read frame length %d: %w", i, err)
		}
		if block.VideoFrames[i].Length > model.MaxFrameSize {
			return model.TimestampBlock{}, fmt.Errorf("frame too large (%d bytes)", block.VideoFrames[i].Length)
		}

		data := make([]byte, int(block.VideoFrames[i].Length))
		if _, err := io.ReadFull(file, data); err != nil {
			return model.TimestampBlock{}, fmt.Errorf("read frame data %d: %w", i, err)
		}
		block.VideoFrames[i].Data = data
	}

	if header.ExtLength > 0 {
		block.ExtData = make([]byte, int(header.ExtLength))
		if _, err := io.ReadFull(file, block.ExtData); err != nil {
			return model.TimestampBlock{}, fmt.Errorf("read ext data: %w", err)
		}
	}
	return block, nil
}
