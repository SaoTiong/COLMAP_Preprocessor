package dwvo

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"dwvo2colmap/internal/model"
)

func readHeader(file *os.File) (model.Header, error) {
	var header model.Header

	magic := make([]byte, len(model.MagicBytes))
	if _, err := io.ReadFull(file, magic); err != nil {
		return header, err
	}
	if string(magic) != model.MagicBytes {
		return header, fmt.Errorf("invalid magic bytes: %q", string(magic))
	}
	if err := binary.Read(file, binary.LittleEndian, &header); err != nil {
		return header, err
	}
	return header, nil
}

func readNullTerminatedString(r io.Reader) (string, error) {
	buf := make([]byte, 0, 16)
	scratch := make([]byte, 1)
	for {
		_, err := r.Read(scratch)
		if err != nil {
			return "", err
		}
		if scratch[0] == 0 {
			return string(buf), nil
		}
		buf = append(buf, scratch[0])
		if len(buf) > 1024 {
			return "", fmt.Errorf("bus ID string too long")
		}
	}
}
