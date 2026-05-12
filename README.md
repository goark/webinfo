# [webinfo] -- Extract metadata from web pages

[![ci status](https://github.com/goark/webinfo/workflows/ci/badge.svg)](https://github.com/goark/webinfo/actions)
[![codeql status](https://github.com/goark/webinfo/workflows/CodeQL/badge.svg)](https://github.com/goark/webinfo/actions)
[![GitHub license](https://img.shields.io/badge/license-Apache%202-blue.svg)](https://raw.githubusercontent.com/goark/webinfo/main/LICENSE)
[![GitHub release](https://img.shields.io/github/release/goark/webinfo.svg)](https://github.com/goark/webinfo/releases/latest)
[![Go reference](https://pkg.go.dev/badge/github.com/goark/webinfo.svg)](https://pkg.go.dev/github.com/goark/webinfo)

`webinfo` extracts common metadata (title, description, canonical, image, etc.)
from web pages and provides helpers to download images and generate thumbnails.

## Design goals

- Keep metadata extraction simple and deterministic.
- Use clear precedence rules for HTML/meta parsing.
- Provide practical image utilities with minimal API surface.
- Keep context-aware network operations as the default style.

## Development

### Requirements

- Go 1.25.10 or later
- [Task](https://taskfile.dev/) command (local tool for this repository)

### Local validation

```text
task test
task govulncheck
```

Run all maintenance tasks:

```text
task
```

## CI Workflows

- `ci`: lint (`golangci-lint` with `gosec`), tests, and `govulncheck`
- `CodeQL`: scheduled and push/PR static analysis

## Usage

### Install and import

```bash
go get github.com/goark/webinfo@latest
```

```go
import "github.com/goark/webinfo"
```

### Fetch metadata

```go
ctx := context.Background()
info, err := webinfo.Fetch(ctx, "https://example.com", "")
if err != nil {
  return err
}
fmt.Println(info.Title, info.Description)
```

### Download image and thumbnail

```go
imgPath, err := info.DownloadImage(ctx, "images", true)
if err != nil {
  return err
}

thumbPath, err := info.DownloadThumbnail(ctx, "thumbnails", 150, false)
if err != nil {
  return err
}

imgBytes, err := info.ImageBytes(ctx)
if err != nil {
  return err
}
fmt.Println(len(imgBytes))
```

### Public API

- `Fetch(ctx, rawURL, userAgent)` extracts metadata from a page.
- `(*Webinfo).ImageBytes(ctx)` downloads `Webinfo.ImageURL` into memory.
- `(*Webinfo).DownloadImage(ctx, destDir, temporary)` downloads `Webinfo.ImageURL`.
- `(*Webinfo).DownloadThumbnail(ctx, destDir, width, temporary)` creates a resized thumbnail.

## Behavior notes

- `Fetch` uses explicit precedence for metadata extraction:
  - title: `title` -> `twitter:title` -> `og:title`
  - description: `meta[name=description]` -> `twitter:description` -> `og:description`
  - image: `twitter:image` -> `og:image`
- `DownloadImage` resolves extension in this order:
  1. URL path extension
  2. response `Content-Type`
  3. sniff first 512 bytes (`http.DetectContentType`)
  4. fallback `.img`
- `DownloadThumbnail` uses width `150` when `width <= 0`.
- `ImageBytes` reads the full response body into memory; very large images can increase memory usage.

## Error handling

This package wraps errors with `github.com/goark/errs` and attaches context
values such as `url`, `path`, and `dir`.

## Modules Requirement Graph

[![dependency.png](./dependency.png)](./dependency.png)

[webinfo]: https://github.com/goark/webinfo "goark/webinfo"
