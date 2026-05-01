package model

type DWVOIndex struct {
	FilePath  string
	Header    Header
	Blocks    []BlockRef
	CameraIDs []string
}

type BlockRef struct {
	Offset    int64
	Timestamp uint32
}

type ExportConfig struct {
	InputFile       string
	WorkspaceRoot   string
	ProjectName     string
	CalibrationFile string
	PrintProgress   bool
	TargetFPS       int
	StartBlock      int
	EndBlock        int
	Camera0BusID    string
	Camera1BusID    string
	SourceFPS       int
	SourceWidth     int
	SourceHeight    int
	SourceNCameras  int
	ExcludeRanges   []BlockRange
}

type BlockRange struct {
	Start int
	End   int
}

type RigConfig []RigConfigGroup

type RigConfigGroup struct {
	Cameras []RigConfigCamera `json:"cameras"`
}

type RigConfigCamera struct {
	ImagePrefix           string    `json:"image_prefix"`
	RefSensor             bool      `json:"ref_sensor,omitempty"`
	CamFromRigRotation    []float64 `json:"cam_from_rig_rotation,omitempty"`
	CamFromRigTranslation []float64 `json:"cam_from_rig_translation,omitempty"`
}
