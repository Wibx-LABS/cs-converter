<p align="center">
  <img src="repo-assets/go-fucking-lang.png" alt="Go Image Extractor Logo" width="300">
</p>

# Image Extractor Utility

A high-performance command-line tool written in Go to extract image URLs from Excel (`.xlsx`) or CSV (`.csv`) spreadsheets, download them concurrently, convert them to Base64 Data URIs, and structure them in an output folder.

This utility runs entirely in-memory and **does not modify the input spreadsheet**.

---

## Features

- **Concurrent Downloading**: Uses a channel-based worker pool (defaults to 20 workers) to download images in parallel.
- **De-duplication & Caching**: Leverages a thread-safe cache to download duplicate URLs exactly once.
- **High-Resolution Scaling**: Automatically transforms original image URLs to fetch high-resolution retina variants (e.g. `image@4x.png`).
- **Resilient Fallback**: Automatically falls back to downloading the original `@1x` variant if the `@4x` asset is missing or private (HTTP 403/404).
- **Spreadsheet Parsing**: Automatically parses all worksheets in Excel files (using `excelize/v2`) and CSV files (with automatic delimiter detection).
- **Intelligent File Naming**: Dynamically matches cell coordinates with row identifiers (like reward IDs, names, or slugs) and column headers to output clean, trace-friendly filenames.

---

## Outputs

The tool writes all extracted assets to a targeted output directory (default: `./output_images/`):

```text
output_images/
├── manifest.json      # Master JSON mapping original URL -> Base64 data & local file paths
├── base64/            # Directory containing raw Base64 Data URI string files (.txt)
└── binary/            # Directory containing downloaded raw image files (.png, .jpg, etc.)
```

### Manifest Schema Example (`manifest.json`):
```json
{
  "https://assets.bonuz.com/prizes/archie-projeto-completo/prize-photo.png": {
    "base64_data": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAB...",
    "mime_type": "image/png",
    "binary_file": "binary/archie-projeto-completo_photo_href.png",
    "base64_file": "base64/archie-projeto-completo_photo_href.txt",
    "actual_url": "https://assets.bonuz.com/prizes/archie-projeto-completo/prize-photo@4x.png"
  }
}
```

---

## Developer Guide & Automation Integration

### 1. Requirements & Prerequisites
- **To compile/develop**: Go 1.20+ (can be downloaded from [golang.org](https://golang.org/)).
- **To run**: The compiled native binary does **not** require Go or any external packages/runtimes installed.

### 2. Compilation
Compile the Go source code into a self-contained, native executable binary:

**For macOS (Intel/Apple Silicon):**
```bash
go build -o image_extractor main.go
```

**For Linux (production servers/containers):**
```bash
GOOS=linux GOARCH=amd64 go build -o image_extractor main.go
```

**For Windows:**
```bash
GOOS=windows GOARCH=amd64 go build -o image_extractor.exe main.go
```

### 3. Command-Line Arguments
The executable accepts the following flags:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--input` | String | *Required* | Path to the input Excel (`.xlsx`) or CSV (`.csv`) file. |
| `--output` | String | `output_images` | Directory path where output assets should be written. |
| `--column` | String | `photo/href` | Filters extraction to matching column headers (case-insensitive substring match). Set to `""` to scan all columns. |
| `--scale` | String | `4x` | High-res scale variant to request (e.g. `2x`, `3x`, `4x`). Set to `""` or `1x` to fetch original resolution. |
| `--workers` | Integer | `20` | Maximum concurrent download workers. |

### 4. Integration Examples

#### Bash/Cron Automation:
A script running on a monthly cron schedule can fetch the latest spreadsheet, run the extractor, and clean up:
```bash
#!/usr/bin/env bash
set -euo pipefail

INPUT_FILE="Clube Bora _ Recompensas da Nuvem Minu.xlsx"
OUTPUT_DIR="./extracted_assets"

# 1. Run the extractor
./image_extractor \
  --input "$INPUT_FILE" \
  --output "$OUTPUT_DIR" \
  --column "photo/href" \
  --scale "4x" \
  --workers 30

# 2. Extract and post-process manifest.json
# (e.g., loading base64 strings or pushing images to a cloud storage bucket)
cat "$OUTPUT_DIR/manifest.json" | jq -r 'keys[]' | head -n 5
```

#### Node.js Child Process Wrapper:
If the dev team has an existing Node.js runner/automation:
```javascript
const { execFile } = require('child_process');
const fs = require('fs');

const args = [
  '--input', 'Clube Bora _ Recompensas da Nuvem Minu.xlsx',
  '--output', './output_images',
  '--column', 'photo/href',
  '--scale', '4x'
];

execFile('./image_extractor', args, (error, stdout, stderr) => {
  if (error) {
    console.error(`Execution failed: ${error.message}`);
    process.exit(1);
  }
  console.log(`Extractor Output:\n${stdout}`);
  
  // Read generated manifest JSON
  const manifest = JSON.parse(fs.readFileSync('./output_images/manifest.json', 'utf8'));
  console.log(`Processed ${Object.keys(manifest).length} unique images!`);
});
```

### 5. Exit Codes
- `0`: Success (scanned cells, downloaded images, and generated output successfully).
- `1` / `non-zero`: Failures (missing required flags, spreadsheet parsing error, directory creation failed).
