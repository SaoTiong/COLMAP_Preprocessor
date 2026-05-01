package dwvo

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"dwvo2colmap/internal/model"
)

func BuildIndex(filePath string) (model.DWVOIndex, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return model.DWVOIndex{}, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	header, err := readHeader(file)
	if err != nil {
		return model.DWVOIndex{}, fmt.Errorf("read header: %w", err)
	}

	index := model.DWVOIndex{
		FilePath: filePath,
		Header:   header,
		Blocks:   make([]model.BlockRef, 0, 4096),
	}

	for {
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return model.DWVOIndex{}, fmt.Errorf("get offset: %w", err)
		}

		var ts uint32
		if err := binary.Read(file, binary.LittleEndian, &ts); err != nil {
			if err == io.EOF {
				break
			}
			if err == io.ErrUnexpectedEOF {
				break
			}
			return model.DWVOIndex{}, fmt.Errorf("read timestamp at offset %d: %w", offset, err)
		}

		cameraIDs, err := skipFrames(file, header.NCameras)
		if err != nil {
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				break
			}
			return model.DWVOIndex{}, fmt.Errorf("read block at offset %d: %w", offset, err)
		}

		if len(index.CameraIDs) == 0 {
			index.CameraIDs = cameraIDs
		}

		if header.ExtLength > 0 {
			if _, err := file.Seek(int64(header.ExtLength), io.SeekCurrent); err != nil {
				if err == io.EOF {
					break
				}
				return model.DWVOIndex{}, fmt.Errorf("skip ext block at offset %d: %w", offset, err)
			}
		}

		index.Blocks = append(index.Blocks, model.BlockRef{
			Offset:    offset,
			Timestamp: ts,
		})
	}

	return index, nil
}

func skipFrames(file *os.File, nCameras uint8) ([]string, error) {
	cameraIDs := make([]string, 0, nCameras)
	for i := 0; i < int(nCameras); i++ {
		busID, err := readNullTerminatedString(file)
		if err != nil {
			return nil, err
		}
		cameraIDs = append(cameraIDs, busID)

		var length uint32
		if err := binary.Read(file, binary.LittleEndian, &length); err != nil {
			return nil, err
		}
		if length > model.MaxFrameSize {
			return nil, fmt.Errorf("frame too large: %d", length)
		}
		if _, err := file.Seek(int64(length), io.SeekCurrent); err != nil {
			return nil, err
		}
	}
	return cameraIDs, nil
}
