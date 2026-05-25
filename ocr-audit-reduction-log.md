# Blackboard Discussion Log: OCR Audit Reduction

- **Date**: 2026-05-25T20:27:00Z
- **Status**: Active

## 1. Problem Decomposition & Objectives
Our objective is to lower the number of manual verifications the user has to perform by utilizing OCR (Optical Character Recognition) to pre-verify images. 
Currently, the user has to manually review 100% of the images in the spreadsheet one-by-one.
We want to use OCR to:
- Detect the presence of the product's expected price (Valor) on the image.
- Detect the presence of the brand name (Aliança/Marca) on the image.
- Automatically approve or mark as "safe" the items where OCR matches the expected data, so the user only has to review "flagged" or "unverified" items.

### Challenges:
1. **CORS Restrictions**: Browser-based OCR (Tesseract.js) requires raw pixel/byte access. In the client-side HTML, if the image hosting server (`assets.bonuz.com`) does not return CORS wildcards or headers, drawing the image to a canvas (required for OCR) throws a DOM Security Error.
2. **Accuracy & Speed**: Browser-based OCR on high-res images can be slow and block the single event-loop thread.
3. **Pre-processing**: Raw images often have low contrast or complex backgrounds that make OCR noisy.

---

## 2. Debate Points

### Point 1: Execution Environment (Client-side HTML vs Go CLI Backend)

#### Hypothesis (Senior Dev)
> **Senior Dev**: I propose implementing the OCR directly in the browser using Tesseract.js. When the spreadsheet is loaded, we can run Tesseract.js in a Web Worker for each row. If Tesseract detects the price (e.g. `50`) or brand name, we auto-mark it as approved, saving the user from clicking. This keeps everything in a single file (`Auditor de Imagens.html`) and doesn't require compiling/installing anything on the host.

#### Critique (Senior Architect)
> **Senior Architect**: Running OCR in the browser has a critical failure mode: **CORS (Cross-Origin Resource Sharing)**. The image URLs point to `https://assets.bonuz.com/...`. Unless the S3/CDN bucket configuration explicitly allows the origin of the local file (which is `file://` or a local development server) with `Access-Control-Allow-Origin: *`, Tesseract.js will fail to read the image bytes. We will get canvas taint security errors, and the OCR will fail completely.
> Furthermore, running OCR concurrently on 400+ images in the browser will consume massive CPU resources, causing the browser tab to lag or crash.
> Doing OCR in the Go CLI backend (`main.go`) is far more robust:
> 1. No CORS limitations. Go can download and open the images locally.
> 2. Better pre-processing options (we can resize/grayscale/threshold using Go image libraries).
> 3. We can output the OCR results directly into the generated manifest or intermediate CSV file, which the HTML tool then consumes.

#### Logical Audit & Verdict (CTO)
> **CTO**: The Architect is correct regarding the CORS security model. Browser-based canvas manipulation of external images is locked down by default. If we cannot guarantee CORS wildcards on the asset domain, client-side OCR is a non-starter.
> However, we must consider the target user: "the DEVs that will be using this script within their automation."
> If the Go script does the heavy lifting, it can extract the text from the downloaded images using a local Tesseract installation (via `tesseract` CLI command execution or library bind), write the extracted text into the metadata `manifest.json` (or add a column to a temporary CSV), and the HTML frontend can read this pre-extracted text.
> Let's analyze the algorithmic cost. Running local OCR in Go can run in a worker pool. The user's system has `tesseract` installed, or we can check if it is available. If it is not, we can fall back.
> Let's debate if we can implement a local CLI-based OCR wrapper in Go.
> Verdict: The Go backend is the optimal place to run OCR because it bypasses CORS and runs in compiled, multi-threaded native code. Let's design the Go CLI script to run OCR on the downloaded images and save the output in the JSON manifest.

---

### Point 2: OCR Text Verification & Auto-Filtering Logic

#### Hypothesis (Senior Dev)
> **Senior Dev**: Once the OCR extracts the raw text from the image, we can run a simple matching logic in JavaScript in the HTML frontend:
> 1. Price Matching: Extract numbers from the spreadsheet's expected value (e.g., `R$ 50` -> `50`). Search the OCR text for that number.
> 2. Brand Matching: Clean the brand name (lowercase, strip accents) and check if it is a substring of the OCR text.
> 3. If both match, we default the item state to "Aprovado" and hide it from the manual review list by default (or show a badge "Auto-Aprovado: Confirma?").

#### Critique (Senior Architect)
> **Senior Architect**: Pure substring match is prone to false negatives due to OCR misspellings (e.g. `Bacio di Latte` read as `Bacio di Lotte` or `Bac1o`). We should use a fuzzy string matching algorithm like Levenshtein distance or a simple token overlap check for the brand names.
> For the price, we should extract the digits and decimal value and search for currency-formatted numbers in the OCR output.
> Also, we should not completely hide auto-approved items, but rather place them in an "Auto-Approved" tab or list so the user can verify them in bulk or skip them entirely if they trust the OCR, only showing the "High Risk / Failed OCR" items in the main queue.

#### Logical Audit & Verdict (CTO)
> **CTO**: Let's formalize the matching rules:
> - **Price Matching**: Clean both expected and OCR strings of non-digit characters (except decimal separator) and check for numeric matches. For instance, if expected price is `50`, we search for `50` or `50.00` or `50,00` or `5000` (in case of comma omission).
> - **Brand Matching**: Clean strings to alphanumeric lowercase. Calculate the Jaro-Winkler or Levenshtein distance, or check if the brand tokens overlap significantly (e.g. at least 70% of brand words exist in OCR text).
> Let's implement these verification helpers in the HTML JavaScript code since it is easily adjustable and inspectable. The HTML will load the `manifest.json` or the OCR results, calculate the score, and group items into "Aprovado via OCR" and "Necessita Revisão".
> Verdict: Run OCR in Go CLI, write raw text to `manifest.json`. Perform the matching and UI categorisation in the HTML frontend.

---

## 3. Resolve Blocked Point & Technical Discovery
> **CTO**: We investigated the environmental constraints and made key technical discoveries:
> 1. **No Local Tesseract CLI**: Running `which tesseract` returned that Tesseract is not installed on the system, making a binary invocation approach via Go `os/exec` block.
> 2. **CORS Restrictions on CDN**: We curl-tested valid asset URLs (e.g. `https://assets.bonuz.com/...`) and verified that the CDN distribution does NOT return `Access-Control-Allow-Origin` headers. This blocks direct remote image byte/canvas reading in the browser.
> 3. **The Data URI/Base64 Solution**: The Go script already downloads assets and compiles them into a JSON manifest (`manifest.json`) containing Base64 data strings.
> Since Data URIs (Base64) have no origin restrictions, they bypass CORS security sandboxes entirely.
> Therefore, we can allow the user to load the generated `manifest.json` into the HTML page. If loaded:
> - The HTML frontend maps URLs to Base64 data URIs.
> - We can render these Base64 images to canvas, perform optional pixel pre-processing, and run client-side `Tesseract.js` in a background worker.
> - This keeps the Go CLI 100% CGO-free and lightweight (cross-compiles instantly), avoiding any external system dependencies, while enabling robust automated client-side OCR pre-verification.
>
> Verdict: Implement optional `manifest.json` upload in the HTML frontend. If provided, run client-side OCR on the Base64 image data.

---

## 4. Final Blueprint & Consensus

### 1. File Upload Enhancements
- Expand the frontend UI to include a secondary, optional upload drop zone / input file selector for the `manifest.json` file.

### 2. OCR Pre-Processing & Execution
- If a manifest is uploaded, map image links to Base64 data URIs.
- Implement canvas pre-processing (grayscale conversion and simple binarization thresholding) to clean up noise and maximize Tesseract.js read accuracy.
- Load Tesseract.js (already loaded via CDN) and run a background worker to extract text on a row-by-row basis as the user kicks off the audit.

### 3. Automated Validation Matching Logic
- **Price Match**: Extract numerical digits from the expected price (e.g., `R$ 50,00` -> `50`) and match against digits/numbers found in the OCR output.
- **Brand Match**: Clean the brand name (lowercase, alphanumeric only) and check if it has a high substring overlap or Levenshtein/token distance matching the OCR text.
- If both match, automatically mark the item status as `Approved (OCR)` and flag it.

### 4. UI Segmentation
- Instead of showing all items in a single list, display a summary dashboard:
  - `Necessitam Revisão Manual` (OCR failed, mismatched, or no manifest provided).
  - `Aprovados via OCR` (Price and Brand matched successfully).
- The user can review the manual queue first, and optionally look at or bulk-approve the OCR-verified list.

**Approved by:**
- [x] CTO (PhD Math/CS)
- [x] Senior Architect
- [x] Senior Developer
