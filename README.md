# [webinfo] -- Extract metadata and structured information from web pages

[![lint status](https://github.com/goark/webinfo/workflows/lint/badge.svg)](https://github.com/goark/webinfo/actions)
[![GitHub license](https://img.shields.io/badge/license-Apache%202-blue.svg)](https://raw.githubusercontent.com/goark/webinfo/master/LICENSE)
[![GitHub release](http://img.shields.io/github/release/goark/webinfo.svg)](https://github.com/goark/webinfo/releases/latest)

[`webinfo`][webinfo] is a small Go module that extracts common metadata from web pages and provides utilities
to download representative images and create thumbnails.

## Quick overview

- **Package**: `webinfo`
- **Repository**: `github.com/goark/webinfo`
- **Purpose**: fetch page metadata (title, description, canonical, image, etc.) and download images

## Features

- Fetch page metadata with `Fetch` (handles encodings and meta tag precedence).
- Download an image referenced by `Webinfo.ImageURL` using `(*Webinfo).DownloadImage`.
- Create a thumbnail from the referenced image using `(*Webinfo).DownloadThumbnail`.

## Install

Use Go modules (Go 1.25+ as used by the project):

```bash
go get github.com/goark/webinfo@latest
```

## Basic usage

Example showing fetch and download thumbnail (error handling omitted for brevity):

```go
package main

import (
    "context"
    "fmt"

    "github.com/goark/webinfo"
)

func main() {
    ctx := context.Background()
    // Fetch metadata for a page (empty UA uses default)
    info, err := webinfo.Fetch(ctx, "https://text.baldanders.info/", "")
    if err != nil {
        fmt.Printf("error detail:\n%+v\n", err)
        return
    }

    // Download thumbnail: width 150, to directory "thumbnails", permanent file
    thumbPath, err := info.DownloadThumbnail(ctx, "thumbnails", 150, false)
    if err != nil {
        fmt.Printf("error detail:\n%+v\n", err)
        return
    }
    fmt.Println("thumbnail saved:", thumbPath)
}
```

### API notes

- `Fetch(ctx, url, userAgent)` — Parse and extract metadata. Pass an empty userAgent to use the module default.
- `(*Webinfo).DownloadImage(ctx, destDir, temporary)` — Download the image in `Webinfo.ImageURL` and save it. If
  `temporary` is true (or `destDir` is empty), a temporary file is created.
- `(*Webinfo).DownloadThumbnail(ctx, destDir, width, temporary)` — Download the referenced image and produce a
  thumbnail resized to `width` pixels (height is preserved by aspect ratio). If `destDir` is empty the method
  creates a temporary file; when `temporary` is false the thumbnail file is named based on the original image
  name with `-thums` appended before the extension.

Note on defaults and test hooks:

- **Default width**: If `width <= 0` is passed to `DownloadThumbnail`, the method uses a default width of 150 pixels.
- **Extension detection**: `DownloadImage` determines an output extension from the URL path, the response
  `Content-Type` (via `mime.ExtensionsByType`), or by sniffing up to the first 512 bytes with `http.DetectContentType`.
- **Test hooks / injection points**: For easier testing the package exposes a few package-level variables that
  tests can override:
  - `createFile`: used to create temporary or permanent files (wraps `os.CreateTemp` / `os.Create`). Override to
    simulate file-creation failures.
  - `decodeImage`: wrapper around `image.Decode` used by `DownloadThumbnail` — override to simulate decode results
    (for example, to return a zero-dimension image).
  - `outputImage`: encoder that writes the thumbnail image to disk (wraps `jpeg.Encode`, `png.Encode`, etc.).
    Override to simulate encoder failures.

These hooks are intended for tests and let callers reproduce rare I/O or encoding failures without changing
production behavior.

## Test examples

Below are short examples showing how to override the package-level hooks from a test to simulate failures.
These snippets are intended for `*_test.go` files and assume the usual `testing` and `net/http/httptest` helpers.

1) Simulate thumbnail temporary-file creation failure (override `createFile`):

```go
// in your test function
orig := createFile
defer func() { createFile = orig }()
createFile = func(temp bool, dir, pattern string) (*os.File, error) {
  // fail only for thumbnail temp pattern
  if temp && strings.Contains(pattern, "webinfo-thumb-") {
    return nil, errors.New("simulated thumbnail temp create failure")
  }
  return orig(temp, dir, pattern)
}

// then call the method under test
_, err := info.DownloadThumbnail(ctx, t.TempDir(), 50, true)
// assert err != nil
```

2) Simulate a zero-dimension decoded image (override `decodeImage`):

```go
origDecode := decodeImage
defer func() { decodeImage = origDecode }()
decodeImage = func(r io.Reader) (image.Image, string, error) {
  // return an image with zero width to hit the origW==0 error path
  return image.NewRGBA(image.Rect(0, 0, 0, 10)), "png", nil
}

_, err := info.DownloadThumbnail(ctx, t.TempDir(), 50, true)
// assert err != nil
```

3) Simulate encoder failure when writing thumbnails (override `outputImage`):

```go
origOut := outputImage
defer func() { outputImage = origOut }()
outputImage = func(dst *os.File, src *image.RGBA, format string) error {
  return errors.New("simulated encode failure")
}

_, err := info.DownloadThumbnail(ctx, t.TempDir(), 50, true)
// assert err != nil
```

Notes:
- Ensure your test imports include `errors`, `io`, `image`, and `strings` as needed.
- Restore the original variables with `defer` to avoid cross-test interference.
- These examples are intentionally minimal — adapt them to your test fixtures (httptest servers, temp dirs, etc.).

### Error handling

The package uses `github.com/goark/errs` for wrapping errors with contextual keys (e.g. `url`, `path`, `dir`).
Callers should inspect returned errors accordingly.

### Tests & development

- Run all tests: `go test ./...`
- The repository includes `Taskfile.yml` tasks for common workflows; see that file for CI/test commands.

## Modules Requirement Graph

[![dependency.png](./dependency.png)](./dependency.png)

[webinfo]: https://github.com/goark/webinfo "goark/webinfo: Extract metadata and structured information from web pages"
