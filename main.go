package main

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
)

// TargetCell stores metadata for a sheet cell containing an image URL
type TargetCell struct {
	URL        string
	Identifier string
	ColHeader  string
	RowIdx     int
	SheetName  string
}

// DownloadResult holds the outcome of an image download attempt
type DownloadResult struct {
	Base64Data  string
	BinaryBytes []byte
	ContentType string
	Extension   string
	ActualURL   string
	Err         error
}

func main() {
	inputFlag := flag.String("input", "", "Path to the input Excel (.xlsx) or CSV (.csv) file (required)")
	outputFlag := flag.String("output", "output_images", "Directory to write the extracted images")
	workersFlag := flag.Int("workers", 20, "Number of concurrent download workers")
	columnFlag := flag.String("column", "photo/href", "Filter by specific column header name (case-insensitive, empty for all columns)")
	scaleFlag := flag.String("scale", "4x", "Image scale to request (e.g. 2x, 3x, 4x, or empty/1x for original)")
	flag.Parse()

	if *inputFlag == "" {
		log.Fatal("Error: --input parameter is required. Use --help for usage details.")
	}

	startTime := time.Now()

	// 1. Parse spreadsheet to extract target cells containing image URLs
	log.Printf("Scanning file: %s ...", *inputFlag)
	var targets []TargetCell
	var err error

	ext := strings.ToLower(filepath.Ext(*inputFlag))
	if ext == ".xlsx" {
		targets, err = parseXLSX(*inputFlag, *columnFlag)
	} else if ext == ".csv" {
		targets, err = parseCSV(*inputFlag, *columnFlag)
	} else {
		log.Fatalf("Error: unsupported file format %s. Only .xlsx and .csv are supported.", ext)
	}

	if err != nil {
		log.Fatalf("Failed to parse file: %v", err)
	}

	log.Printf("Scan complete. Found %d total cells containing image URLs.", len(targets))
	if len(targets) == 0 {
		log.Println("No image URLs found to process. Exiting.")
		return
	}

	// 2. Identify unique URLs
	uniqueURLsMap := make(map[string]bool)
	var uniqueURLs []string
	for _, t := range targets {
		if !uniqueURLsMap[t.URL] {
			uniqueURLsMap[t.URL] = true
			uniqueURLs = append(uniqueURLs, t.URL)
		}
	}
	log.Printf("Detected %d unique image URLs to download.", len(uniqueURLs))

	// 3. Set up directory structures
	base64Dir := filepath.Join(*outputFlag, "base64")
	binaryDir := filepath.Join(*outputFlag, "binary")
	for _, dir := range []string{base64Dir, binaryDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create output directory %s: %v", dir, err)
		}
	}

	// 4. Concurrently download unique URLs
	log.Printf("Starting download pool with %d concurrent workers...", *workersFlag)
	
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var cache sync.Map // maps url (string) -> *DownloadResult
	
	urlChan := make(chan string, len(uniqueURLs))
	for _, u := range uniqueURLs {
		urlChan <- u
	}
	close(urlChan)

	var wg sync.WaitGroup
	var downloadCount int
	var failCount int
	var progressMutex sync.Mutex

	for i := 0; i < *workersFlag; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range urlChan {
				data, contentType, ext, actualURL, err := fetchImageWithFallback(client, u, *scaleFlag)
				
				res := &DownloadResult{
					ContentType: contentType,
					Extension:   ext,
					ActualURL:   actualURL,
					Err:         err,
				}

				if err == nil {
					res.BinaryBytes = data
					b64Str := base64.StdEncoding.EncodeToString(data)
					res.Base64Data = fmt.Sprintf("data:%s;base64,%s", contentType, b64Str)
				}

				cache.Store(u, res)

				progressMutex.Lock()
				if err != nil {
					failCount++
					log.Printf("[-] Failed to download: %s | Error: %v", u, err)
				} else {
					downloadCount++
					totalFinished := downloadCount + failCount
					if totalFinished%50 == 0 || totalFinished == len(uniqueURLs) {
						log.Printf("[Progress] %d/%d URLs processed.", totalFinished, len(uniqueURLs))
					}
				}
				progressMutex.Unlock()
			}
		}()
	}

	wg.Wait()
	log.Printf("Downloads complete. Success: %d, Failed: %d.", downloadCount, failCount)

	// 5. Write outputs
	log.Println("Writing base64 and binary files to output directories...")
	
	manifest := make(map[string]interface{})
	usedFilenames := make(map[string]bool)
	var writeSuccessCount int
	var writeFailCount int

	for _, target := range targets {
		val, ok := cache.Load(target.URL)
		if !ok {
			continue
		}
		res := val.(*DownloadResult)
		if res.Err != nil {
			continue // Skip writing files for URLs that failed to download
		}

		// Generate a collision-resistant filename
		colName := sanitizeFilename(target.ColHeader)
		identifier := sanitizeFilename(target.Identifier)
		filenameBase := fmt.Sprintf("%s_%s", identifier, colName)

		finalName := filenameBase
		counter := 1
		for usedFilenames[finalName] {
			counter++
			finalName = fmt.Sprintf("%s_%d", filenameBase, counter)
		}
		usedFilenames[finalName] = true

		// Write binary image file
		binaryFilename := fmt.Sprintf("%s.%s", finalName, res.Extension)
		binaryPath := filepath.Join(binaryDir, binaryFilename)
		err := os.WriteFile(binaryPath, res.BinaryBytes, 0644)
		if err != nil {
			log.Printf("Warning: failed to write binary file %s: %v", binaryPath, err)
			writeFailCount++
			continue
		}

		// Write base64 text file
		base64Filename := fmt.Sprintf("%s.txt", finalName)
		base64Path := filepath.Join(base64Dir, base64Filename)
		err = os.WriteFile(base64Path, []byte(res.Base64Data), 0644)
		if err != nil {
			log.Printf("Warning: failed to write base64 file %s: %v", base64Path, err)
			writeFailCount++
			continue
		}

		writeSuccessCount++

		// Track in manifest mapping
		manifest[target.URL] = map[string]string{
			"base64_data": res.Base64Data,
			"mime_type":   res.ContentType,
			"binary_file": filepath.Join("binary", binaryFilename),
			"base64_file": filepath.Join("base64", base64Filename),
			"actual_url":  res.ActualURL,
		}
	}

	// 6. Write central manifest.json
	manifestPath := filepath.Join(*outputFlag, "manifest.json")
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Printf("Error packaging manifest JSON: %v", err)
	} else {
		err = os.WriteFile(manifestPath, manifestJSON, 0644)
		if err != nil {
			log.Printf("Error writing manifest.json: %v", err)
		} else {
			log.Printf("Manifest written to %s", manifestPath)
		}
	}

	elapsed := time.Since(startTime)
	log.Printf("=================== SUMMARY ===================")
	log.Printf("Execution time:      %s", elapsed)
	log.Printf("Total cell references: %d", len(targets))
	log.Printf("Unique URLs scanned:   %d", len(uniqueURLs))
	log.Printf("Successful downloads:  %d", downloadCount)
	log.Printf("Failed downloads:      %d", failCount)
	log.Printf("Files written (each):  %d (binary & base64)", writeSuccessCount)
	log.Printf("===============================================")
}

// parseXLSX reads worksheets from Excel file and extracts target cells
func parseXLSX(path string, colFilter string) ([]TargetCell, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var allTargets []TargetCell
	sheets := f.GetSheetList()

	for _, sheetName := range sheets {
		rows, err := f.GetRows(sheetName)
		if err != nil {
			log.Printf("Warning: failed to get rows in sheet %s: %v", sheetName, err)
			continue
		}

		targets, err := extractFromRecords(rows, sheetName, colFilter)
		if err != nil {
			log.Printf("Warning: failed to extract from sheet %s: %v", sheetName, err)
			continue
		}
		allTargets = append(allTargets, targets...)
	}

	return allTargets, nil
}

// parseCSV reads records from CSV file and extracts target cells
func parseCSV(path string, colFilter string) ([]TargetCell, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	separator := ','
	firstLine := ""
	if idx := strings.Index(string(data), "\n"); idx != -1 {
		firstLine = string(data[:idx])
	} else {
		firstLine = string(data)
	}

	// Simple heuristic to check if CSV is semicolon-separated (common in European/Brazilian locales)
	if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
		separator = ';'
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = separator
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	return extractFromRecords(records, "CSV", colFilter)
}

// extractFromRecords searches rows for image URLs and associates them with headers and row identifiers
func extractFromRecords(records [][]string, sheetName string, colFilter string) ([]TargetCell, error) {
	if len(records) == 0 {
		return nil, nil
	}

	headers := records[0]
	idColIdx := findIdentifierColumn(headers)

	// Regex to match full HTTP/S image URL
	rxURL := regexp.MustCompile(`(?i)^https?://[^\s?#]+\.(png|jpg|jpeg|gif|webp|svg|bmp)(?:\?[^\s#]*)?(?:#[^\s]*)?$`)
	
	var targets []TargetCell

	for rIdx := 1; rIdx < len(records); rIdx++ {
		row := records[rIdx]

		var identifier string
		if idColIdx >= 0 && idColIdx < len(row) {
			identifier = strings.TrimSpace(row[idColIdx])
		}
		if identifier == "" {
			identifier = fmt.Sprintf("row_%d", rIdx+1)
		}

		for cIdx, val := range row {
			val = strings.TrimSpace(val)
			if rxURL.MatchString(val) {
				colHeader := fmt.Sprintf("col_%d", cIdx+1)
				if cIdx < len(headers) {
					h := strings.TrimSpace(headers[cIdx])
					if h != "" {
						colHeader = h
					}
				}

				// Filter by column header if specified
				if colFilter != "" {
					hClean := strings.ToLower(colHeader)
					fClean := strings.ToLower(colFilter)
					if !strings.Contains(hClean, fClean) {
						continue
					}
				}

				targets = append(targets, TargetCell{
					URL:        val,
					Identifier: identifier,
					ColHeader:  colHeader,
					RowIdx:     rIdx + 1,
					SheetName:  sheetName,
				})
			}
		}
	}

	return targets, nil
}

// findIdentifierColumn looks for a known naming column name in headers
func findIdentifierColumn(headers []string) int {
	candidates := []string{
		"id da recompensa",
		"id",
		"name",
		"slug",
		"title",
		"nome da recompensa",
		"nome",
		"recompensa",
		"identifier",
	}

	for _, candidate := range candidates {
		for idx, h := range headers {
			hClean := strings.ToLower(strings.TrimSpace(h))
			if hClean == candidate {
				return idx
			}
		}
	}
	return -1
}

// fetchImage fetches the binary data of an image URL and detects metadata
func fetchImage(client *http.Client, urlStr string) ([]byte, string, string, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("http error: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}

	ext := getExtensionFromMime(contentType, urlStr)

	return data, contentType, ext, nil
}

// fetchImageWithFallback attempts to fetch the image URL transformed with the scale, and falls back to the original URL if it fails
func fetchImageWithFallback(client *http.Client, urlStr string, scale string) ([]byte, string, string, string, error) {
	transformed := transformURL(urlStr, scale)

	if transformed != urlStr {
		data, contentType, ext, err := fetchImage(client, transformed)
		if err == nil {
			return data, contentType, ext, transformed, nil
		}
	}

	data, contentType, ext, err := fetchImage(client, urlStr)
	return data, contentType, ext, urlStr, err
}

// transformURL inserts the @scale prefix (like @4x) before the file extension in the URL path
func transformURL(urlStr string, scale string) string {
	if scale == "" || scale == "1x" {
		return urlStr
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr
	}

	// Only transform if it has a path and extension
	ext := filepath.Ext(u.Path)
	if ext == "" {
		return urlStr
	}

	// Check if path already contains a scale to avoid double suffix
	if strings.Contains(u.Path, "@1.5x") || strings.Contains(u.Path, "@2x") || strings.Contains(u.Path, "@3x") || strings.Contains(u.Path, "@4x") {
		return urlStr
	}

	// Add scale before extension, e.g. path/file.png -> path/file@4x.png
	base := strings.TrimSuffix(u.Path, ext)
	u.Path = fmt.Sprintf("%s@%s%s", base, scale, ext)
	return u.String()
}

// getExtensionFromMime normalizes extension based on Content-Type and URL path
func getExtensionFromMime(mimeType string, urlStr string) string {
	mimeType = strings.ToLower(strings.Split(mimeType, ";")[0])
	switch mimeType {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	case "image/bmp":
		return "bmp"
	}

	// Fallback to reading extension from URL
	u, err := url.Parse(urlStr)
	if err == nil {
		ext := strings.ToLower(filepath.Ext(u.Path))
		if ext != "" {
			return strings.TrimPrefix(ext, ".")
		}
	}
	return "png"
}

// sanitizeFilename strips out characters unsafe for file paths
func sanitizeFilename(s string) string {
	// Remove invalid chars
	reg := regexp.MustCompile(`[^a-zA-Z0-9_\-\.]`)
	res := reg.ReplaceAllString(s, "_")
	// Collapse multiple underscores
	res = regexp.MustCompile(`_+`).ReplaceAllString(res, "_")
	res = strings.Trim(res, "_-.")
	if res == "" {
		res = "unnamed"
	}
	return res
}
