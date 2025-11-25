Purpose
-------
This file gives concise, actionable guidance for AI coding agents working on the `webinfo` Go module.

What this project does
----------------------
Extracts metadata (title, description, canonical, image, etc.) from web pages and provides utilities
to fetch and save representative images and create thumbnails.

Quick entry points
------------------
- **Primary package**: `webinfo` — key files:
  - `fetch.go` (core `Fetch` function and encoding handling)
  - `webinfo.go` (`Webinfo` type, `DownloadImage`, and `DownloadThumbnail`)
  - `errs.go` (error sentinel values)
  - `fetch_test.go` (behavioral tests and examples)
- **Go module**: `go 1.25` (see `go.mod`).

Developer workflows
-------------------
- Run full CI/test workflow using the Taskfile (recommended if `task` is installed):
  - `task test` — runs `go mod verify`, `go test -shuffle on ./...`, `govulncheck`, and `golangci-lint-v2` as configured in `Taskfile.yml`.
- Quick test: `go test ./...` (useful during fast iteration).
- Prepare module: `go mod tidy -v -go=1.25` (mirrors `prepare` in `Taskfile.yml`).

Project-specific conventions and patterns
----------------------------------------
- Error handling: uses `github.com/goark/errs`. Prefer `errs.Wrap(err, errs.WithContext("key", val))` for context-rich errors and `errs.Join` when combining close errors in `defer`.
- HTTP fetching: uses `github.com/goark/fetch`. Typical pattern:
  - Parse URL with `fetch.URL(...)`.
  - Use `fetch.New(...).GetWithContext(ctx, parsed, fetch.WithRequestHeaderSet("User-Agent", ua))`.
- Default User-Agent: `getUserAgent("")` returns a dummy UA string. Functions accept a `userAgent` param but fall back to this default.
- Encoding: `Fetch` peeks the first 1024 bytes and uses `charset.DetermineEncoding` and `encoding.GetEncoding(name)` to decode response bodies before HTML parsing — preserve this approach when touching parsing logic.
- HTML parsing: `goquery` is used to select head elements and meta tags. Extraction precedence is explicit in `fetch.go` (title → `twitter:title`/`og:title`, description → `twitter:description`/`og:description`, image → `twitter:image`/`og:image`). Follow this precedence in code changes or tests.

Image download and thumbnail notes
---------------------------------
- `DownloadImage` (in `webinfo.go`) downloads `w.ImageURL` and saves it to disk. It determines the output file extension using this order:
  1) extension from the URL path,
  2) extensions inferred from the response `Content-Type` header,
  3) sniffing the first up to 512 bytes via `http.DetectContentType`,
  4) fallback to `.img` if none found.
  When sniffing, the read bytes are prepended back into the response body with `io.MultiReader` so the full image is written.
- `DownloadThumbnail` (added to `webinfo.go`) downloads the original image (via `DownloadImage`), resizes it to a requested width (preserving aspect ratio) and writes a thumbnail. Implementation notes:
  - The code currently uses a local nearest-neighbor scaler (no external `x/image/draw` dependency) to avoid adding module requirements.
  - The method accepts `width` (default 150 when <= 0), `destDir`, and `temporary` flags. When `destDir` is empty the method forces creation of a temporary file.
  - When `temporary` is false, the thumbnail filename is derived from the original image basename with `-thums` appended before the extension.

I/O and cleanup
----------------
- Response bodies and files are closed; close errors are wrapped/joined with any existing error.
- Errors encountered while parsing the URL, fetching, reading, sniffing, creating directories/files, or copying data are wrapped with contextual information (e.g. `"url"`, `"path"`, `"dir"`, `"file"`) using the `errs` package.

Tests and examples
------------------
- Tests use `net/http/httptest` for deterministic responses (encoding tests use `golang.org/x/text/encoding/japanese`). Inspect `fetch_test.go` for examples of:
  - Redirect handling and validation of `Location`.
  - Encoding tests for Shift_JIS and ISO-2022-JP.
  - Verifying `User-Agent` header usage.
- Example usage patterns to follow when adding code or tests:
  - Fetch: `info, err := Fetch(ctx, "https://example.com", "")` — empty UA uses the default.
  - Download image: `outPath, err := w.DownloadImage(ctx, "images", true)`
  - Download thumbnail: `thumbPath, err := w.DownloadThumbnail(ctx, "thumbnails", 150, false)`

External dependencies & integration points
----------------------------------------
- Key dependencies in `go.mod`: `github.com/goark/fetch`, `github.com/goark/errs`, `github.com/PuerkitoBio/goquery`, `golang.org/x/text` (encodings).
- The repository intentionally avoids adding `golang.org/x/image/draw` as a dependency; if you need higher-quality scaling consider adding it and updating `go.mod` and tests.
- The `Taskfile.yml` runs additional tools: `govulncheck`, `golangci-lint-v2`, and (optionally) `nancy` via `depm` — keep CI tool invocations in sync when adding dependencies.

When modifying public APIs
-------------------------
- Maintain existing error-wrapping conventions (`errs.Wrap`, `errs.WithContext`).
- Preserve encoding detection behavior and the 1024-byte peek in `Fetch` unless a clear, tested performance reason exists.
- Preserve `DownloadImage`'s extension-detection order and the behavior of `temporary` vs permanent files. When adding `DownloadThumbnail` behavior or changing file-naming semantics, update tests accordingly.

Where to look next (high-value files)
-------------------------------------
- `fetch.go` — how pages are fetched, decoded and parsed.
- `webinfo.go` — `Webinfo` type, `DownloadImage`, and `DownloadThumbnail` implementations.
- `fetch_test.go` — canonical tests and examples you should mirror for new behaviors.
- `errs.go` and `go.mod` — error constants and dependency hints.
- `Taskfile.yml` — canonical developer/test/lint workflow.

If anything above is unclear or you want small patches, test templates, or a CI-safe refactor suggestion, tell me which area to expand and I will iterate.
