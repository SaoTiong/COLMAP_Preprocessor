package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"dwvo2colmap/internal/dwvo"
	"dwvo2colmap/internal/model"
)

func WriteCOLMAPRange(index model.DWVOIndex, cfg model.ExportConfig) error {
	projectRoot := filepath.Join(cfg.WorkspaceRoot, cfg.ProjectName)
	rigRoot := filepath.Join(projectRoot, "rig")
	cam0Dir := filepath.Join(rigRoot, "cam0")
	cam1Dir := filepath.Join(rigRoot, "cam1")

	for _, dir := range []string{cam0Dir, cam1Dir, filepath.Join(projectRoot, "sparse")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	selectedBlocks := selectBlockIndices(cfg.StartBlock, cfg.EndBlock, cfg.SourceFPS, cfg.TargetFPS, cfg.ExcludeRanges)
	total := len(selectedBlocks)
	if total == 0 {
		return fmt.Errorf("no frames selected for export")
	}
	cam0List := make([]string, 0, total)
	cam1List := make([]string, 0, total)
	for frameNum := 1; frameNum <= total; frameNum++ {
		fileName := fmt.Sprintf("%06d.jpg", frameNum)
		cam0List = append(cam0List, filepath.ToSlash(filepath.Join("cam0", fileName)))
		cam1List = append(cam1List, filepath.ToSlash(filepath.Join("cam1", fileName)))
	}

	// Create metadata files immediately when export starts.
	if err := writeImageList(filepath.Join(projectRoot, "cam0.txt"), cam0List); err != nil {
		return err
	}
	if err := writeImageList(filepath.Join(projectRoot, "cam1.txt"), cam1List); err != nil {
		return err
	}
	calibData, err := loadCalibrationData(cfg.CalibrationFile)
	if err != nil {
		return err
	}
	if err := writeRigConfig(filepath.Join(projectRoot, "rig_config.json"), calibData.RigConfig); err != nil {
		return err
	}
	pipelineDir := filepath.Join(projectRoot, "pipeline")
	if err := os.MkdirAll(pipelineDir, 0755); err != nil {
		return err
	}
	if err := writePipelineScripts(pipelineDir, calibData.Cam0Params, calibData.Cam1Params); err != nil {
		return err
	}

	file, err := os.Open(index.FilePath)
	if err != nil {
		return fmt.Errorf("open DWVO file: %w", err)
	}
	defer file.Close()

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 12 {
		workers = 12
	}

	type frameTask struct {
		frameNum int
		cam0Data []byte
		cam1Data []byte
	}
	type frameResult struct {
		frameNum int
		cam0Out  string
		cam1Out  string
		err      error
	}

	taskCh := make(chan frameTask, workers*2)
	resultCh := make(chan frameResult, workers*2)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				fileName := fmt.Sprintf("%06d.jpg", task.frameNum)
				cam0Out := filepath.Join(cam0Dir, fileName)
				cam1Out := filepath.Join(cam1Dir, fileName)

				err := writeFrameAsJPEG(task.cam0Data, cam0Out)
				if err == nil {
					err = writeFrameAsJPEG(task.cam1Data, cam1Out)
				}
				resultCh <- frameResult{
					frameNum: task.frameNum,
					cam0Out:  cam0Out,
					cam1Out:  cam1Out,
					err:      err,
				}
			}
		}()
	}

	var firstErr error
	var resultErrMu sync.Mutex
	doneCollect := make(chan struct{})
	go func() {
		defer close(doneCollect)
		completed := 0
		for res := range resultCh {
			completed++
			if res.err != nil {
				resultErrMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("frame %d: %w", res.frameNum, res.err)
				}
				resultErrMu.Unlock()
				continue
			}
			if cfg.PrintProgress {
				fmt.Printf("%06d/%06d saved to %s and %s\n", res.frameNum, total, res.cam0Out, res.cam1Out)
			}
		}
	}()

	frameNum := 1
	for _, blockIdx := range selectedBlocks {
		block := index.Blocks[blockIdx]
		tsBlock, err := dwvo.ReadBlockAtFromFile(file, index.Header, block.Offset)
		if err != nil {
			return fmt.Errorf("read block %d: %w", blockIdx, err)
		}

		cam0Frame, ok := findFrameByBusID(tsBlock.VideoFrames, cfg.Camera0BusID)
		if !ok {
			return fmt.Errorf("camera %q not found in block %d", cfg.Camera0BusID, blockIdx)
		}
		cam1Frame, ok := findFrameByBusID(tsBlock.VideoFrames, cfg.Camera1BusID)
		if !ok {
			return fmt.Errorf("camera %q not found in block %d", cfg.Camera1BusID, blockIdx)
		}

		taskCh <- frameTask{
			frameNum: frameNum,
			cam0Data: cam0Frame.Data,
			cam1Data: cam1Frame.Data,
		}
		frameNum++
	}

	close(taskCh)
	wg.Wait()
	close(resultCh)
	<-doneCollect

	if firstErr != nil {
		return firstErr
	}
	return nil
}

func selectBlockIndices(start, end, sourceFPS, targetFPS int, excludes []model.BlockRange) []int {
	if start > end {
		return nil
	}
	stride := 1
	if targetFPS > 0 && sourceFPS > targetFPS {
		stride = sourceFPS / targetFPS
		if sourceFPS%targetFPS != 0 {
			stride++
		}
		if stride < 1 {
			stride = 1
		}
	}
	selected := make([]int, 0, (end-start)/stride+1)
	for i := start; i <= end; i += stride {
		if isExcluded(i, excludes) {
			continue
		}
		selected = append(selected, i)
	}
	return selected
}

func isExcluded(idx int, excludes []model.BlockRange) bool {
	for _, ex := range excludes {
		if idx >= ex.Start && idx <= ex.End {
			return true
		}
	}
	return false
}

func findFrameByBusID(frames []model.VideoFrame, busID string) (model.VideoFrame, bool) {
	for _, frame := range frames {
		if frame.BusID == busID {
			return frame, true
		}
	}
	return model.VideoFrame{}, false
}

func writeFrameAsJPEG(frameData []byte, outPath string) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Fast path: DWVO frames are JPEG already, so write bytes directly.
	if len(frameData) > 3 && frameData[0] == 0xFF && frameData[1] == 0xD8 {
		if _, err := out.Write(frameData); err == nil {
			return nil
		}
		if _, err := out.Seek(0, 0); err == nil {
			_ = out.Truncate(0)
		}
	}

	decoded, _, err := image.Decode(bytes.NewReader(frameData))
	if err != nil {
		return err
	}
	return jpeg.Encode(out, decoded, &jpeg.Options{Quality: 95})
}

func writeImageList(path string, entries []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, entry := range entries {
		if _, err := file.WriteString(entry + "\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeRigConfig(path string, cfg model.RigConfig) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

type fumiCalibration struct {
	Calibration struct {
		Cameras []struct {
			Model struct {
				PtrWrapper struct {
					Data struct {
						Parameters struct {
							F  calibParam `json:"f"`
							Ar calibParam `json:"ar"`
							CX calibParam `json:"cx"`
							CY calibParam `json:"cy"`
							K1 calibParam `json:"k1"`
							K2 calibParam `json:"k2"`
							P1 calibParam `json:"p1"`
							P2 calibParam `json:"p2"`
						} `json:"parameters"`
					} `json:"data"`
				} `json:"ptr_wrapper"`
			} `json:"model"`
			Transform struct {
				Rotation struct {
					RX float64 `json:"rx"`
					RY float64 `json:"ry"`
					RZ float64 `json:"rz"`
				} `json:"rotation"`
				Translation struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
					Z float64 `json:"z"`
				} `json:"translation"`
			} `json:"transform"`
		} `json:"cameras"`
	} `json:"Calibration"`
}

type calibParam struct {
	Val float64 `json:"val"`
}

type calibrationData struct {
	RigConfig  model.RigConfig
	Cam0Params string
	Cam1Params string
}

func loadCalibrationData(calibrationFile string) (calibrationData, error) {
	if calibrationFile == "" {
		return calibrationData{}, fmt.Errorf("missing calibration json path")
	}

	raw, err := os.ReadFile(calibrationFile)
	if err != nil {
		return calibrationData{}, fmt.Errorf("read calibration json: %w", err)
	}

	var calib fumiCalibration
	if err := json.Unmarshal(raw, &calib); err != nil {
		return calibrationData{}, fmt.Errorf("parse calibration json: %w", err)
	}
	if len(calib.Calibration.Cameras) < 2 {
		return calibrationData{}, fmt.Errorf("calibration json requires at least 2 cameras")
	}

	cam0Params := toColmapOPENCVParams(calib.Calibration.Cameras[0])
	cam1Params := toColmapOPENCVParams(calib.Calibration.Cameras[1])

	cam1 := calib.Calibration.Cameras[1]
	qw, qx, qy, qz := eulerXYZToQuaternion(
		cam1.Transform.Rotation.RX,
		cam1.Transform.Rotation.RY,
		cam1.Transform.Rotation.RZ,
	)

	return calibrationData{
		RigConfig: model.RigConfig{
			{
				Cameras: []model.RigConfigCamera{
					{
						ImagePrefix: "cam0/",
						RefSensor:   true,
					},
					{
						ImagePrefix:           "cam1/",
						CamFromRigRotation:    []float64{qw, qx, qy, qz},
						CamFromRigTranslation: []float64{cam1.Transform.Translation.X, cam1.Transform.Translation.Y, cam1.Transform.Translation.Z},
					},
				},
			},
		},
		Cam0Params: cam0Params,
		Cam1Params: cam1Params,
	}, nil
}

func eulerXYZToQuaternion(rx, ry, rz float64) (qw, qx, qy, qz float64) {
	cx := math.Cos(rx * 0.5)
	sx := math.Sin(rx * 0.5)
	cy := math.Cos(ry * 0.5)
	sy := math.Sin(ry * 0.5)
	cz := math.Cos(rz * 0.5)
	sz := math.Sin(rz * 0.5)

	qw = cx*cy*cz - sx*sy*sz
	qx = sx*cy*cz + cx*sy*sz
	qy = cx*sy*cz - sx*cy*sz
	qz = cx*cy*sz + sx*sy*cz
	return qw, qx, qy, qz
}

func toColmapOPENCVParams(cam struct {
	Model struct {
		PtrWrapper struct {
			Data struct {
				Parameters struct {
					F  calibParam `json:"f"`
					Ar calibParam `json:"ar"`
					CX calibParam `json:"cx"`
					CY calibParam `json:"cy"`
					K1 calibParam `json:"k1"`
					K2 calibParam `json:"k2"`
					P1 calibParam `json:"p1"`
					P2 calibParam `json:"p2"`
				} `json:"parameters"`
			} `json:"data"`
		} `json:"ptr_wrapper"`
	} `json:"model"`
	Transform struct {
		Rotation struct {
			RX float64 `json:"rx"`
			RY float64 `json:"ry"`
			RZ float64 `json:"rz"`
		} `json:"rotation"`
		Translation struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
			Z float64 `json:"z"`
		} `json:"translation"`
	} `json:"transform"`
}) string {
	p := cam.Model.PtrWrapper.Data.Parameters
	fy := p.F.Val
	fx := p.F.Val * p.Ar.Val
	values := []float64{fx, fy, p.CX.Val, p.CY.Val, p.K1.Val, p.K2.Val, p.P1.Val, p.P2.Val}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strconv.FormatFloat(v, 'f', 6, 64))
	}
	return strings.Join(out, ",")
}

func writePipelineScripts(pipelineDir, cam0Params, cam1Params string) error {
	scripts := map[string]string{
		"feature_extractionL.sh": fmt.Sprintf(`set -euo pipefail

PROJECT="$(cd .. && pwd)"


echo "start feature_extractor cam0"
colmap feature_extractor \
  --database_path "${PROJECT}/database.db" \
  --image_path "${PROJECT}/rig" \
  --image_list_path "${PROJECT}/cam0.txt" \
  --ImageReader.camera_model OPENCV \
  --ImageReader.camera_params %q\
  --FeatureExtraction.type ALIKED_N16ROT \
  --AlikedExtraction.min_score 0.2 \
  --FeatureExtraction.use_gpu 1 \
  --ImageReader.single_camera 1 \
  --Rerun.enabled 1

echo "finish feature_extractor cam0"
`, cam0Params),

		"feature_extractionR.sh": fmt.Sprintf(`set -euo pipefail

PROJECT="$(cd .. && pwd)"


echo "start feature_extractor cam1"
colmap feature_extractor \
  --database_path "${PROJECT}/database.db" \
  --image_path "${PROJECT}/rig" \
  --image_list_path "${PROJECT}/cam1.txt" \
  --ImageReader.camera_model OPENCV \
  --ImageReader.camera_params %q\
  --FeatureExtraction.type ALIKED_N16ROT \
  --AlikedExtraction.min_score 0.2 \
  --FeatureExtraction.use_gpu 1 \
  --ImageReader.single_camera 1 \
  --Rerun.enabled 1

echo "finish feature_extractor cam1"
`, cam1Params),

		"rig_configuration.sh": `set -euo pipefail

PROJECT="$(cd .. && pwd)"

echo "start rig_configurator"
colmap rig_configurator \
  --database_path "${PROJECT}/database.db" \
  --rig_config_path "${PROJECT}/rig_config.json"
`,

		"sequential_matching.sh": `set -euo pipefail

PROJECT="$(cd .. && pwd)"


echo "start sequential_matcher"
colmap sequential_matcher \
  --database_path "${PROJECT}/database.db" \
  --image_path "${PROJECT}/rig" \
  --FeatureMatching.type ALIKED_LIGHTGLUE \
  --AlikedMatching.lightglue_min_score 0.5 \
  --TwoViewGeometry.max_error 2 \
  --SequentialMatching.loop_detection 1 \
  --SequentialMatching.loop_detection_period 20 \
  --SequentialMatching.loop_detection_num_images 50 \
  --SequentialMatching.loop_detection_num_images_after_verification 5 \
  --SequentialMatching.loop_detection_max_num_features 100 \
  --Rerun.enabled 1 \
  --Rerun.spawn 0

echo "finish sequential_matcher"
`,

		"incremental_mapper.sh": `set -euo pipefail

PROJECT="$(cd .. && pwd)"

echo "start mapper"
colmap mapper \
  --database_path "${PROJECT}/database.db" \
  --Mapper.ba_refine_sensor_from_rig 1 \
  --Mapper.ba_refine_focal_length 1 \
  --Mapper.ba_refine_extra_params 1 \
  --image_path "${PROJECT}/rig" \
  --output_path "${PROJECT}/sparse" \
  --Mapper.ba_use_gpu 1 \
  --Mapper.snapshot_path "${PROJECT}/snapshots/" \
  --Mapper.snapshot_frames_freq 100

echo "finish mapper"
`,

		"global_mapper.sh": `set -euo pipefail

PROJECT="$(cd .. && pwd)"


echo "start global mapper"
 colmap global_mapper \
   --database_path ${PROJECT}/database.db \
   --image_path ${PROJECT}/rig \
   --output_path ${PROJECT}/sparse \
   --GlobalMapper.ba_refine_focal_length 1 \
   --GlobalMapper.ba_refine_principal_point 0 \
   --GlobalMapper.ba_refine_extra_params 1 \
   --GlobalMapper.ba_refine_sensor_from_rig 1 \
   --GlobalMapper.gp_use_gpu 1 \
   --GlobalMapper.ba_ceres_use_gpu 1 \
   --Rerun.enabled 1 \
   --Rerun.spawn 1
 echo "stop global mapper"
`,

		"dense_mapper.sh": `set -euo pipefail

PROJECT="$(cd .. && pwd)"


echo "start image undistortion"
 colmap image_undistorter \
   --input_path ${PROJECT}/sparse/0/ \
   --image_path ${PROJECT}/rig/ \
   --output_path ${PROJECT}/dense/ \
   --output_type COLMAP
echo "finish image undistortion"

echo "start stereo patch"
 colmap patch_match_stereo \
   --workspace_path ${PROJECT}/dense/ \
   --PatchMatchStereo.geom_consistency 1 \
   --PatchMatchStereo.filter 1 \
   --PatchMatchStereo.cache_size 64
echo "finish stereo patch"

echo "start fusion"
colmap stereo_fusion \
  --workspace_path ${PROJECT}/dense --workspace_format COLMAP --input_type geometric --output_path ${PROJECT}/dense/dense.ply
echo "finish fusion"
`,
	}

	for name, content := range scripts {
		path := filepath.Join(pipelineDir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}
