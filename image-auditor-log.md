# Blackboard Discussion Log: Image Auditor Frontend

- **Date**: 2026-05-25T16:45:00-03:00
- **Status**: Active

## 1. Problem Decomposition & Objectives
We need to create a single-file HTML/CSS/JS frontend (`Auditor de Imagens.html`) matching the premium design language of `Calculadora Wibx - UAU.html` (Clash Display font, green accents `#22ff7b`, dark mode theme `#070707`).
Objectives:
1. **Drag and Drop Interface**: Accept `.xlsx` and `.csv` files and parse them client-side in the browser.
2. **Automated Image OCR Auditing**:
   - For each row containing an image URL (such as `photo/href`), load the image.
   - Use Tesseract.js (in-browser OCR) to extract text from the image.
   - Run validation logic: check if the expected price/value (from columns like `faceValue` or `Valor oficial da recompensa`) is shown on the image.
   - Identify mismatching text, missing numbers, or errors (failed loads).
3. **Manual Review Interface (Approval Queue)**:
   - Present rows that failed the automated OCR validation to the user one-by-one.
   - Show the image side-by-side with metadata: `alliance/name`, `title`, `description`, expected price/value, and the OCR extracted text.
   - Provide "Approve" (force pass) and "Reject" buttons.
4. **Data Export**: Allow downloading a report of the audit results (e.g., CSV or JSON) showing which URLs are approved/valid for the Go image extractor script.

---

## 2. Debate Points

### Point 1: Client-Side Excel and CSV Parsing

#### Hypothesis (Senior Dev)
> **Senior Dev**: Since it must be a single HTML file running client-side, we should load two lightweight external libraries from CDNs:
> 1. `xlsx.full.min.js` (SheetJS) to read `.xlsx` files:
>    `<script src="https://cdn.jsdelivr.net/npm/xlsx@0.18.5/dist/xlsx.full.min.js"></script>`
> 2. `papaparse.min.js` to parse `.csv` files:
>    `<script src="https://cdn.jsdelivr.net/npm/papaparse@5.4.1/papaparse.min.js"></script>`
> This allows us to parse Excel and CSV files into standard JSON arrays in a few lines of code.

#### Critique (Senior Architect)
> **Senior Architect**: Yes, CDN loading for SheetJS and PapaParse is perfect for a standalone utility HTML file. However, we must handle different Excel sheet layouts. The spreadsheet could have multiple sheets (as we saw earlier, our XLSX has 5 sheets, and the image links are in sheet 4, `Informações e Imagens`). 
> The UI should let the user choose which sheet they want to audit if the file has multiple sheets, rather than just choosing the first sheet automatically. This prevents auditing the wrong data.

#### Logical Audit & Verdict (CTO)
> **CTO**: Excellent point on sheet selection. When a multi-sheet `.xlsx` file is uploaded, the UI must display a dropdown list of sheets, allowing the user to select the correct sheet (e.g. defaulting to a sheet containing "imagens" or "prêmios" in the name, but letting them override it).
> Verdict: Load SheetJS and PapaParse via CDN. Add an interactive sheet selector dropdown when a multi-sheet XLSX is uploaded.

---

### Point 2: OCR Execution & CORS Handling

#### Hypothesis (Senior Dev)
> **Senior Dev**: We can import Tesseract.js via CDN:
> `<script src="https://cdn.jsdelivr.net/npm/tesseract.js@5/dist/tesseract.min.js"></script>`
> For each row, we will load the image URL into an HTML Image object, draw it on a hidden canvas to extract the image data, and pass the canvas or image URL to `Tesseract.recognize()`.
> To prevent CORS issues when drawing external URLs on canvas, we must set `img.crossOrigin = "anonymous"`.

#### Critique (Senior Architect)
> **Senior Architect**: Public S3 buckets (like `assets.bonuz.com` and `s3.sa-east-1.amazonaws.com`) often support CORS, but if they do not, `crossOrigin = "anonymous"` will fail and trigger a security error, rendering the canvas "dirty" and blocking OCR.
> To handle this gracefully:
> 1. We should display a warning in the UI if an image fails to load due to CORS or network errors.
> 2. For images that fail to load, we automatically place them into the "Manual Review Queue" as a separate category: "Erro de Carregamento / CORS". The user can then audit them manually since they will be visible to the user as a standard `<img>` tag (which doesn't require CORS unless drawn on a canvas).
> 3. Tesseract.js loading can be slow. We must process the images in batches or sequentially with a visual progress bar (e.g., "Auditing image 12 of 400...") so the browser doesn't freeze.

#### Logical Audit & Verdict (CTO)
> **CTO**: Sequentially running OCR is key. Running 1000 OCRs concurrently in a single browser tab will crash the JavaScript heap. We will process images sequentially or with a concurrency limit of 2 (using workers).
> To improve validation accuracy, we must pre-process the image if possible (e.g., grayscale, increase contrast) when drawing to canvas.
> For CORS failures: yes, any load failure or CORS error must immediately push the row to the manual review queue with a clear "CORS/Load Failure" tag.
> Verdict: Sequentially process images via Tesseract.js. Draw to canvas with `crossOrigin = "anonymous"`. Catch errors and route them to the manual queue.

---

### Point 3: OCR Validation Rules (Price Match)

#### Hypothesis (Senior Dev)
> **Senior Dev**: We want to make sure the price shown in the image matches the spreadsheet value. 
> If the spreadsheet has `faceValue` or `Valor oficial da recompensa` = `20`, we should look for "20", "R$ 20", or "20,00" in the OCR text.
> I will write a function to normalize both the spreadsheet price and the OCR text:
> - Strip currency symbols, commas, and dots from the spreadsheet price.
> - Search for this numeric value in the OCR text.

#### Critique (Senior Architect)
> **Senior Architect**: Price matches are sometimes false negatives because OCR might read "20" as "200" or fail to read commas.
> We must use a regex-based matching algorithm:
> 1. Extract the raw numeric digits from the spreadsheet value (e.g., `24.00` -> `24`).
> 2. Search the OCR text for that number.
> 3. If the spreadsheet value has a decimal (like `24,00`), check both the integer part `24` and the full format.
> 4. We should also search for the alliance name (brand name, e.g. "12 Minutos") or title keywords in the OCR text. If both the price and brand name/title match, it passes. If the price is present in the spreadsheet but missing or different in the OCR, it fails.
> 5. We must show the extracted OCR text side-by-side with the expected value in the manual queue, highlighting matching elements.

#### Logical Audit & Verdict (CTO)
> **CTO**: The comparison algorithm must be mathematically precise but flexible:
> Let $V$ be the target numeric value from the spreadsheet (e.g., `24` or `100`).
> We will search for $V$ in the OCR text using a boundary check to avoid matching parts of larger numbers (e.g., matching `10` inside `100`).
> Regex: `new RegExp(`\\b${V}\\b`)` or checking localized formatting like `R$ V`.
> If $V$ is found in the OCR text, we consider the price validated. If $V$ is not found, we mark it as "Price Mismatch".
> If the image contains no readable text, we mark it as "No text detected (unreadable/blurred)".
> If the brand name (alliance name) is present, we check if at least one keyword matches.
> Verdict: Implement a flexible price and brand matching logic. Categorize failures (e.g., "Divergência de Preço", "Imagem Sem Texto", "Erro de Conexão").

---

### Point 4: Manual Review Dashboard Interface

#### Hypothesis (Senior Dev)
> **Senior Dev**: The manual review interface will be a modal or card presenting one item at a time. It will show the image, the metadata, the OCR text, and buttons "Aprovar" and "Rejeitar".
> Once the user processes all items in the queue, we present a completion screen with a button to download the finalized JSON manifest or CSV.

#### Critique (Senior Architect)
> **Senior Architect**: Yes, a step-by-step wizard interface is highly usable. We should show a counter (e.g., "Item 3 de 15 falhas") and a keyboard shortcut option (e.g., Left Arrow to Reject, Right Arrow to Approve, Space to skip) to make the manual auditing process extremely fast for the user.

#### Logical Audit & Verdict (CTO)
> **CTO**: The keyboard shortcuts are excellent for developer experience (DX).
> We will implement:
> - Centralized dashboard layout:
>   - Section 1: Upload (Drag & Drop zone, sheet selection, column mapping confirmation).
>   - Section 2: Progress (Visual logs of OCR processing, stats of pass/fail/error).
>   - Section 3: Review Queue (Single-item review card, split-screen: left side shows image, right side shows metadata, comparison results, OCR text, and buttons).
>   - Section 4: Export (Button to download the final CSV of approved items).
> Verdict: Full dashboard with Drag & Drop, batch OCR logging, split-screen manual review with keyboard shortcuts, and CSV/JSON export.

---

## 3. Blocked: Critical Uncertainty
*None.*

## 4. Final Blueprint & Consensus
We will implement `Auditor de Imagens.html` in the workspace:
- **Styling**: Single file with inline `<style>` tag, using Clash Display font, dark background `#070707`, cards `#101010` and `#161616`, green accents `#22ff7b` (matching the WiBx brand).
- **Libraries**: Load Tesseract.js, SheetJS, and PapaParse via CDNs.
- **Workflow**:
  1. **Upload**: User drags XLSX/CSV. SheetJS parses sheets. Column selectors are auto-mapped based on header analysis (matching `photo/href`, `alliance/name`, `title`, `description`, `faceValue`).
  2. **Audit Run**: User clicks "Iniciar Auditoria". Tool iterates rows sequentially. It loads the image via canvas, executes Tesseract OCR, checks for price/brand matching, and stores results.
  3. **Queue**: Displays failed rows one-by-one. User reviews and clicks "Aprovar" or "Rejeitar" (or uses keys).
  4. **Export**: User downloads a refined CSV listing only the approved rows, which can then be fed directly to the Go extractor!

**Approved by:**
- [x] CTO (PhD Math/CS)
- [x] Senior Architect
- [x] Senior Developer
