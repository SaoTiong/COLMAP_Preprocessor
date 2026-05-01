package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dwvo2colmap/internal/dwvo"
	"dwvo2colmap/internal/export"
	"dwvo2colmap/internal/model"
	"dwvo2colmap/internal/ui"
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		exitErr(err)
	}

	mode := strings.ToLower(cfg.Mode)
	if mode == "ui" {
		ui.RunLocalWindow(ui.LocalConfig{
			InputFile:   cfg.InputFile,
			OutputRoot:  cfg.OutputRoot,
			ProjectName: cfg.ProjectName,
			CalibFile:   cfg.CalibFile,
		})
		return
	}
	if mode != "cli" {
		exitErr(fmt.Errorf("invalid --mode %q (use ui or cli)", cfg.Mode))
	}

	if cfg.InputFile == "" {
		exitErr(fmt.Errorf("missing required --input for cli mode"))
	}
	if cfg.CalibFile == "" {
		exitErr(fmt.Errorf("missing required --calib for cli mode"))
	}

	index, err := dwvo.BuildIndex(cfg.InputFile)
	if err != nil {
		exitErr(fmt.Errorf("index DWVO: %w", err))
	}

	if len(index.Blocks) == 0 {
		exitErr(fmt.Errorf("no frames found in DWVO file"))
	}
	if len(index.CameraIDs) < 2 {
		exitErr(fmt.Errorf("expected at least 2 cameras, got %d", len(index.CameraIDs)))
	}

	start, end, err := resolveRange(cfg, len(index.Blocks))
	if err != nil {
		exitErr(err)
	}

	workspaceRoot := cfg.OutputRoot
	expCfg := model.ExportConfig{
		InputFile:       cfg.InputFile,
		WorkspaceRoot:   workspaceRoot,
		ProjectName:     cfg.ProjectName,
		CalibrationFile: cfg.CalibFile,
		TargetFPS:       cfg.TargetFPS,
		StartBlock:      start,
		EndBlock:        end,
		Camera0BusID:    index.CameraIDs[0],
		Camera1BusID:    index.CameraIDs[1],
		SourceFPS:       int(index.Header.Format.FPS),
		SourceWidth:     int(index.Header.Format.Width),
		SourceHeight:    int(index.Header.Format.Height),
		SourceNCameras:  int(index.Header.NCameras),
	}

	if err := export.WriteCOLMAPRange(index, expCfg); err != nil {
		exitErr(fmt.Errorf("export COLMAP project: %w", err))
	}

	exported := estimateExportCount(start, end, int(index.Header.Format.FPS), cfg.TargetFPS)
	fmt.Printf("Export complete: %s\n", filepath.Join(workspaceRoot, cfg.ProjectName))
	fmt.Printf("Frames exported: %d (target %d FPS, block range %d..%d)\n", exported, cfg.TargetFPS, start, end)
}

type cliConfig struct {
	Mode        string
	InputFile   string
	OutputRoot  string
	ProjectName string
	CalibFile   string
	StartBlock  int
	EndBlock    int
	TargetFPS   int
}

func parseFlags() (cliConfig, error) {
	cfg := cliConfig{}

	flag.StringVar(&cfg.Mode, "mode", "ui", "Mode: ui or cli")
	flag.StringVar(&cfg.InputFile, "input", "", "Path to input DWVO file")
	flag.StringVar(&cfg.OutputRoot, "out", ".", "Output root folder")
	flag.StringVar(&cfg.ProjectName, "project", "project_name", "COLMAP project folder name")
	flag.StringVar(&cfg.CalibFile, "calib", "", "Path to camera calibration JSON file")
	flag.IntVar(&cfg.StartBlock, "start-block", 0, "Start block index (inclusive)")
	flag.IntVar(&cfg.EndBlock, "end-block", -1, "End block index (inclusive), -1 means last block")
	flag.IntVar(&cfg.TargetFPS, "fps", 5, "Target FPS for export (1-10)")
	flag.Parse()

	if cfg.ProjectName == "" {
		return cfg, fmt.Errorf("project name cannot be empty")
	}
	if cfg.TargetFPS < 1 || cfg.TargetFPS > 10 {
		return cfg, fmt.Errorf("--fps must be between 1 and 10, got %d", cfg.TargetFPS)
	}
	return cfg, nil
}

func resolveRange(cfg cliConfig, totalBlocks int) (int, int, error) {
	start := cfg.StartBlock
	end := cfg.EndBlock
	if end == -1 {
		end = totalBlocks - 1
	}
	if start < 0 || end < 0 {
		return 0, 0, fmt.Errorf("start/end block must be >= 0")
	}
	if start >= totalBlocks || end >= totalBlocks {
		return 0, 0, fmt.Errorf("block range out of bounds: total blocks=%d requested=%d..%d", totalBlocks, start, end)
	}
	if start > end {
		return 0, 0, fmt.Errorf("invalid range: start-block (%d) > end-block (%d)", start, end)
	}
	return start, end, nil
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func estimateExportCount(start, end, sourceFPS, targetFPS int) int {
	if end < start {
		return 0
	}
	stride := 1
	if targetFPS > 0 && sourceFPS > targetFPS {
		stride = sourceFPS / targetFPS
		if sourceFPS%targetFPS != 0 {
			stride++
		}
	}
	count := 0
	for i := start; i <= end; i += stride {
		count++
	}
	return count
}
