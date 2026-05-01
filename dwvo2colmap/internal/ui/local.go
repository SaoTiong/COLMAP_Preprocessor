package ui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"path/filepath"
	"strings"

	"dwvo2colmap/internal/dwvo"
	"dwvo2colmap/internal/export"
	"dwvo2colmap/internal/model"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type LocalConfig struct {
	InputFile   string
	OutputRoot  string
	ProjectName string
	CalibFile   string
}

type localState struct {
	index         *model.DWVOIndex
	currentBlock  int
	selectA       int
	selectB       int
	targetFPS     int
	excludeRanges []model.BlockRange
}

func RunLocalWindow(cfg LocalConfig) {
	a := app.New()
	w := a.NewWindow("dwvo2colmap")
	w.Resize(fyne.NewSize(1280, 860))

	if cfg.OutputRoot == "" {
		cfg.OutputRoot = "."
	}
	if cfg.ProjectName == "" {
		cfg.ProjectName = "project_name"
	}

	state := &localState{
		selectA:   -1,
		selectB:   -1,
		targetFPS: 5,
	}

	leftImage := canvas.NewImageFromImage(nil)
	leftImage.FillMode = canvas.ImageFillContain
	leftImage.SetMinSize(fyne.NewSize(640, 420))

	rightImage := canvas.NewImageFromImage(nil)
	rightImage.FillMode = canvas.ImageFillContain
	rightImage.SetMinSize(fyne.NewSize(640, 420))

	statusLabel := widget.NewLabel("Load a DWVO file to start.")
	cameraLabel := widget.NewLabel("Camera mapping: n/a")
	selectionLabel := widget.NewLabel("Selection: A=-, B=-")

	inputEntry := widget.NewEntry()
	inputEntry.SetPlaceHolder("/path/to/input.dwvo")
	inputEntry.SetText(cfg.InputFile)

	outputEntry := widget.NewEntry()
	outputEntry.SetText(cfg.OutputRoot)

	projectEntry := widget.NewEntry()
	projectEntry.SetText(cfg.ProjectName)

	calibEntry := widget.NewEntry()
	calibEntry.SetPlaceHolder("/path/to/fumi_calibration.json")
	calibEntry.SetText(cfg.CalibFile)

	slider := widget.NewSlider(0, 0)
	slider.Step = 1
	slider.Disable()

	updateSelectionLabel := func() {
		aText := "-"
		bText := "-"
		if state.selectA >= 0 {
			aText = fmt.Sprintf("%d", state.selectA)
		}
		if state.selectB >= 0 {
			bText = fmt.Sprintf("%d", state.selectB)
		}
		selectionLabel.SetText(fmt.Sprintf("Selection: A=%s, B=%s", aText, bText))
	}

	renderBlock := func(blockIdx int) {
		if state.index == nil || blockIdx < 0 || blockIdx >= len(state.index.Blocks) {
			return
		}
		blockRef := state.index.Blocks[blockIdx]
		block, err := dwvo.ReadBlockAt(state.index.FilePath, state.index.Header, blockRef.Offset)
		if err != nil {
			dialog.ShowError(fmt.Errorf("read block %d: %w", blockIdx, err), w)
			return
		}

		leftFrame, ok := findFrameByBusID(block.VideoFrames, state.index.CameraIDs[0])
		if !ok {
			dialog.ShowError(fmt.Errorf("left camera frame missing in block %d", blockIdx), w)
			return
		}
		rightFrame, ok := findFrameByBusID(block.VideoFrames, state.index.CameraIDs[1])
		if !ok {
			dialog.ShowError(fmt.Errorf("right camera frame missing in block %d", blockIdx), w)
			return
		}

		leftDecoded, err := decodeFrame(leftFrame.Data)
		if err != nil {
			dialog.ShowError(fmt.Errorf("decode left frame: %w", err), w)
			return
		}
		rightDecoded, err := decodeFrame(rightFrame.Data)
		if err != nil {
			dialog.ShowError(fmt.Errorf("decode right frame: %w", err), w)
			return
		}

		leftImage.Image = leftDecoded
		rightImage.Image = rightDecoded
		leftImage.Refresh()
		rightImage.Refresh()

		state.currentBlock = blockIdx
		fps := int(state.index.Header.Format.FPS)
		if fps <= 0 {
			fps = 30
		}
		timeSec := float64(blockIdx) / float64(fps)
		statusLabel.SetText(fmt.Sprintf(
			"File: %s | Block: %d/%d | Time: %.3fs | Timestamp: %d",
			filepath.Base(state.index.FilePath),
			blockIdx,
			len(state.index.Blocks)-1,
			timeSec,
			blockRef.Timestamp,
		))
	}

	loadDWVO := func(path string) {
		if path == "" {
			dialog.ShowError(fmt.Errorf("input path is empty"), w)
			return
		}
		idx, err := dwvo.BuildIndex(path)
		if err != nil {
			dialog.ShowError(fmt.Errorf("index DWVO: %w", err), w)
			return
		}
		if len(idx.Blocks) == 0 {
			dialog.ShowError(fmt.Errorf("DWVO contains no blocks"), w)
			return
		}
		if len(idx.CameraIDs) < 2 {
			dialog.ShowError(fmt.Errorf("expected at least 2 cameras, found %d", len(idx.CameraIDs)), w)
			return
		}

		state.index = &idx
		state.currentBlock = 0
		state.selectA = -1
		state.selectB = -1
		updateSelectionLabel()

		cameraLabel.SetText(fmt.Sprintf("Camera mapping: cam0=%s, cam1=%s", idx.CameraIDs[0], idx.CameraIDs[1]))
		slider.Enable()
		slider.Max = float64(len(idx.Blocks) - 1)
		slider.SetValue(0)
		renderBlock(0)
	}

	slider.OnChanged = func(value float64) {
		blockIdx := int(value + 0.5)
		renderBlock(blockIdx)
	}

	openButton := widget.NewButton("Open DWVO", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return
			}
			path := reader.URI().Path()
			_ = reader.Close()
			inputEntry.SetText(path)
			loadDWVO(path)
		}, w)
		fileDialog.Show()
	})

	loadButton := widget.NewButton("Load", func() {
		loadDWVO(inputEntry.Text)
	})

	setAButton := widget.NewButton("Set A", func() {
		if state.index == nil {
			return
		}
		state.selectA = state.currentBlock
		updateSelectionLabel()
	})
	setBButton := widget.NewButton("Set B", func() {
		if state.index == nil {
			return
		}
		state.selectB = state.currentBlock
		updateSelectionLabel()
	})
	jumpAButton := widget.NewButton("Jump A", func() {
		if state.selectA >= 0 {
			slider.SetValue(float64(state.selectA))
		}
	})
	jumpBButton := widget.NewButton("Jump B", func() {
		if state.selectB >= 0 {
			slider.SetValue(float64(state.selectB))
		}
	})
	clearSelectionButton := widget.NewButton("Clear A/B", func() {
		state.selectA = -1
		state.selectB = -1
		updateSelectionLabel()
	})

	pickOutButton := widget.NewButton("Choose Output", func() {
		folderDialog := dialog.NewFolderOpen(func(listableURI fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if listableURI == nil {
				return
			}
			outputEntry.SetText(listableURI.Path())
		}, w)
		folderDialog.Show()
	})

	pickCalibButton := widget.NewButton("Choose Calib", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return
			}
			calibEntry.SetText(reader.URI().Path())
			_ = reader.Close()
		}, w)
		fileDialog.Show()
	})

	// FPS selector (1-10)
	fpsLabel := widget.NewLabel("Target FPS: 5")
	fpsSlider := widget.NewSlider(1, 10)
	fpsSlider.Step = 1
	fpsSlider.SetValue(5)
	fpsSlider.OnChanged = func(value float64) {
		state.targetFPS = int(value + 0.5)
		fpsLabel.SetText(fmt.Sprintf("Target FPS: %d", state.targetFPS))
	}

	// Clip/exclusion zone management
	clipListLabel := widget.NewLabel("Excluded ranges: (none)")

	updateClipLabel := func() {
		if len(state.excludeRanges) == 0 {
			clipListLabel.SetText("Excluded ranges: (none)")
			return
		}
		var parts []string
		for i, r := range state.excludeRanges {
			fps := 30
			if state.index != nil && state.index.Header.Format.FPS > 0 {
				fps = int(state.index.Header.Format.FPS)
			}
			startSec := float64(r.Start) / float64(fps)
			endSec := float64(r.End) / float64(fps)
			parts = append(parts, fmt.Sprintf("#%d: block %d..%d (%.1fs..%.1fs)", i+1, r.Start, r.End, startSec, endSec))
		}
		clipListLabel.SetText("Excluded: " + strings.Join(parts, " | "))
	}

	var clipStartBlock int = -1
	clipStartLabel := widget.NewLabel("Clip start: -")

	clipMarkStartButton := widget.NewButton("Clip Start", func() {
		if state.index == nil {
			return
		}
		clipStartBlock = state.currentBlock
		fps := int(state.index.Header.Format.FPS)
		if fps <= 0 {
			fps = 30
		}
		clipStartLabel.SetText(fmt.Sprintf("Clip start: %d (%.1fs)", clipStartBlock, float64(clipStartBlock)/float64(fps)))
	})

	clipMarkEndButton := widget.NewButton("Clip End (Add)", func() {
		if state.index == nil || clipStartBlock < 0 {
			dialog.ShowError(fmt.Errorf("set clip start first"), w)
			return
		}
		clipEnd := state.currentBlock
		start, end := clipStartBlock, clipEnd
		if start > end {
			start, end = end, start
		}
		// Validate clip is within A..B
		if state.selectA >= 0 && state.selectB >= 0 {
			rangeStart, rangeEnd := state.selectA, state.selectB
			if rangeStart > rangeEnd {
				rangeStart, rangeEnd = rangeEnd, rangeStart
			}
			if start < rangeStart || end > rangeEnd {
				dialog.ShowError(fmt.Errorf("clip range %d..%d is outside A..B (%d..%d)", start, end, rangeStart, rangeEnd), w)
				return
			}
		}
		state.excludeRanges = append(state.excludeRanges, model.BlockRange{Start: start, End: end})
		clipStartBlock = -1
		clipStartLabel.SetText("Clip start: -")
		updateClipLabel()
	})

	undoClipButton := widget.NewButton("Undo Clip", func() {
		if len(state.excludeRanges) > 0 {
			state.excludeRanges = state.excludeRanges[:len(state.excludeRanges)-1]
			updateClipLabel()
		}
	})

	clearClipsButton := widget.NewButton("Clear Clips", func() {
		state.excludeRanges = nil
		clipStartBlock = -1
		clipStartLabel.SetText("Clip start: -")
		updateClipLabel()
	})

	var exportButton *widget.Button
	exportButton = widget.NewButton("Export A..B", func() {
		if state.index == nil {
			dialog.ShowError(fmt.Errorf("load a DWVO file first"), w)
			return
		}
		if state.selectA < 0 || state.selectB < 0 {
			dialog.ShowError(fmt.Errorf("set both A and B before export"), w)
			return
		}
		if calibEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("select calibration json before export"), w)
			return
		}

		start, end := state.selectA, state.selectB
		if start > end {
			start, end = end, start
		}
		sourceFPS := int(state.index.Header.Format.FPS)
		exportCount := estimateExportCount(start, end, sourceFPS, state.targetFPS)

		exportButton.Disable()
		statusLabel.SetText("Exporting selected range...")
		workspaceRoot := outputEntry.Text
		expCfg := model.ExportConfig{
			InputFile:       state.index.FilePath,
			WorkspaceRoot:   workspaceRoot,
			ProjectName:     projectEntry.Text,
			CalibrationFile: calibEntry.Text,
			PrintProgress:   true,
			TargetFPS:       state.targetFPS,
			StartBlock:      start,
			EndBlock:        end,
			Camera0BusID:    state.index.CameraIDs[0],
			Camera1BusID:    state.index.CameraIDs[1],
			SourceFPS:       sourceFPS,
			SourceWidth:     int(state.index.Header.Format.Width),
			SourceHeight:    int(state.index.Header.Format.Height),
			SourceNCameras:  int(state.index.Header.NCameras),
			ExcludeRanges:   state.excludeRanges,
		}
		err := export.WriteCOLMAPRange(*state.index, expCfg)

		exportButton.Enable()
		if err != nil {
			dialog.ShowError(err, w)
			renderBlock(state.currentBlock)
			return
		}
		dialog.ShowInformation(
			"Export Complete",
			fmt.Sprintf(
				"Saved to:\n%s\n\nFrames: ~%d (target %d FPS, %d clips excluded)",
				filepath.Join(workspaceRoot, projectEntry.Text),
				exportCount,
				state.targetFPS,
				len(state.excludeRanges),
			),
			w,
		)
		renderBlock(state.currentBlock)
	})

	topControls := container.NewVBox(
		container.NewBorder(nil, nil, nil, openButton, inputEntry),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, nil, pickOutButton, outputEntry),
			container.NewBorder(nil, nil, widget.NewLabel("Project"), nil, projectEntry),
		),
		container.NewBorder(nil, nil, nil, pickCalibButton, calibEntry),
		container.NewHBox(loadButton, setAButton, setBButton, jumpAButton, jumpBButton, clearSelectionButton),
		container.NewHBox(fpsLabel, fpsSlider),
		container.NewHBox(clipMarkStartButton, clipMarkEndButton, undoClipButton, clearClipsButton, clipStartLabel),
		clipListLabel,
		container.NewHBox(exportButton),
		cameraLabel,
		statusLabel,
		selectionLabel,
	)

	imagePane := container.NewGridWithColumns(
		2,
		container.NewBorder(widget.NewLabel("cam0"), nil, nil, nil, leftImage),
		container.NewBorder(widget.NewLabel("cam1"), nil, nil, nil, rightImage),
	)

	content := container.NewBorder(
		topControls,
		container.NewVBox(
			widget.NewLabel("Timeline"),
			slider,
		),
		nil,
		nil,
		imagePane,
	)
	w.SetContent(content)

	if cfg.InputFile != "" {
		loadDWVO(cfg.InputFile)
	}

	w.ShowAndRun()
}

func decodeFrame(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func findFrameByBusID(frames []model.VideoFrame, busID string) (model.VideoFrame, bool) {
	for _, frame := range frames {
		if frame.BusID == busID {
			return frame, true
		}
	}
	return model.VideoFrame{}, false
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
