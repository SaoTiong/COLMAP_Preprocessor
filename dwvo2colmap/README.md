# dwvo2colmap

Go tool for previewing DWVO and exporting frame ranges into a COLMAP-style workspace.

## Current status

- DWVO header parsing and block indexing
- Native local UI:
  - Open DWVO
  - Scrub timeline frame-by-frame
  - Set A / Set B / Jump A / Jump B / Clear
  - Export selected A..B range (downsampled to 5 FPS)
- CLI block-range export (`start-block` to `end-block`)
- Output layout:
  - `<output_root>/<project>/rig/cam0/*.jpg`
  - `<output_root>/<project>/rig/cam1/*.jpg`
  - `<output_root>/<project>/cam0.txt`
  - `<output_root>/<project>/cam1.txt`
  - `<output_root>/<project>/rig_config.json`
  - `<output_root>/<project>.sh` (COLMAP command script)
- `cam0.txt` / `cam1.txt` uses line format from your sample:
  - `cam0/000001.jpg`
  - `cam1/000001.jpg`

## Build

```bash
cd /home/blake/DWE/dwvo2colmap
go build ./...
```

## Run UI (default mode)

```bash
go run ./cmd/dwvo2colmap --mode ui
```

You can also pre-fill input/output/project from CLI:

```bash
go run ./cmd/dwvo2colmap \
  --mode ui \
  --input /path/to/input.dwvo \
  --out /path/to/output_root \
  --calib /path/to/fumi_calibration.json \
  --project project_name
```

## Run CLI export

```bash
go run ./cmd/dwvo2colmap \
  --mode cli \
  --input /path/to/input.dwvo \
  --out /path/to/output_root \
  --calib /path/to/fumi_calibration.json \
  --project project_name \
  --start-block 0 \
  --end-block -1
```

Notes:
- `--end-block -1` means export through the final block.
- First two cameras in DWVO order are mapped to `cam0` and `cam1`.
