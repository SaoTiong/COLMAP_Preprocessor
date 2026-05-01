package model

const (
	MagicBytes   = "DWE.ai"
	MaxFrameSize = 10 * 1024 * 1024
)

type Version struct {
	Major   uint8
	Minor   uint8
	Nightly uint8
}

type Format struct {
	Width       uint32
	Height      uint32
	PixelFormat uint32
	FPS         uint8
}

type Header struct {
	Version   Version
	NCameras  uint8
	Format    Format
	ExtLength uint32
}

type VideoFrame struct {
	BusID  string
	Length uint32
	Data   []byte
}

type TimestampBlock struct {
	Timestamp   uint32
	VideoFrames []VideoFrame
	ExtData     []byte
}
