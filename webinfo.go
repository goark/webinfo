package webinfo

import (
	"bytes"
	"context"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // register webp decoder

	"github.com/goark/errs"
	"github.com/goark/fetch"
	"golang.org/x/image/draw"
)

// Webinfo stores metadata extracted from a web page and values used for
// follow-up image download operations.
type Webinfo struct {
	URL         string `json:"url,omitempty"`         // Original page URL
	Location    string `json:"location,omitempty"`    // Location
	Canonical   string `json:"canonical,omitempty"`   // Canonical URL
	Title       string `json:"title,omitempty"`       // Page title
	Description string `json:"description,omitempty"` // Meta description
	ImageURL    string `json:"image_url,omitempty"`   // Representative image URL
	UserAgent   string `json:"user_agent,omitempty"`  // User-Agent used to fetch the page
}

// ImageBytes downloads w.ImageURL and returns its contents in memory.
//
// Risk: this method reads the entire response body into memory, so very large
// images can increase memory usage.
//
// Returned errors are wrapped with context and include response close failures.
func (w *Webinfo) ImageBytes(ctx context.Context) (data []byte, err error) {
	if w == nil {
		err = errs.Wrap(ErrNullPointer)
		return
	}
	if w.ImageURL == "" {
		err = errs.Wrap(ErrNoImageURL)
		return
	}

	parsed, uerr := fetch.URL(strings.TrimSpace(w.ImageURL))
	if uerr != nil {
		err = errs.Wrap(uerr, errs.WithContext("url", w.ImageURL))
		return
	}

	resp, ferr := fetch.New(fetch.WithHTTPClient(newHTTPClient())).GetWithContext(
		ctx,
		parsed,
		fetch.WithRequestHeaderSet("User-Agent", getUserAgent(w.UserAgent)),
	)
	if ferr != nil {
		err = errs.Wrap(ferr, errs.WithContext("url", parsed.String()))
		return
	}
	defer func() {
		if cerr := resp.Close(); cerr != nil && cerr != os.ErrClosed {
			err = errs.Join(cerr, err)
		}
	}()

	data, err = io.ReadAll(resp.Body())
	if err != nil {
		err = errs.Wrap(err, errs.WithContext("url", parsed.String()))
		return
	}
	return
}

// DownloadImage downloads w.ImageURL and writes it under destDir.
//
// If temporary is true, or if the URL path has no filename, a temporary file is created.
// Otherwise the output file name is derived from the URL path.
//
// Extension resolution order is:
//  1. URL path extension
//  2. response Content-Type
//  3. sniffed content type from up to 512 bytes
//  4. fallback ".img"
//
// Returned errors are wrapped with context and include cleanup failures.
func (w *Webinfo) DownloadImage(ctx context.Context, destDir string, temporary bool) (outPath string, err error) {
	if w == nil {
		err = errs.Wrap(ErrNullPointer)
		return
	}

	// make directory if needed
	destDir = filepath.Clean(destDir)
	if len(destDir) > 0 {
		if derr := os.MkdirAll(destDir, 0o750); derr != nil {
			err = errs.Wrap(derr, errs.WithContext("destDir", destDir))
			return
		}
	}

	// parse image URL
	if w.ImageURL == "" {
		err = errs.Wrap(ErrNoImageURL)
		return
	}
	parsed, uerr := fetch.URL(strings.TrimSpace(w.ImageURL))
	if uerr != nil {
		err = errs.Wrap(uerr, errs.WithContext("url", w.ImageURL))
		return
	}
	srcPath := path.Clean(parsed.Path)
	srcFname := path.Base(srcPath)
	if srcFname == "" || srcFname == "." || srcFname == "/" {
		srcFname = ""
		temporary = true // use temporary file if no filename in URL
	}
	srcExt := path.Ext(srcFname)

	// fetch image
	resp, ferr := fetch.New(fetch.WithHTTPClient(newHTTPClient())).GetWithContext(
		ctx,
		parsed,
		fetch.WithRequestHeaderSet("User-Agent", getUserAgent(w.UserAgent)),
	)
	if ferr != nil {
		err = errs.Wrap(ferr, errs.WithContext("url", parsed.String()))
		return
	}
	defer func() { // ensure response body closed
		if cerr := resp.Close(); cerr != nil && cerr != os.ErrClosed {
			err = errs.Join(cerr, err)
		}
	}()

	// Try to determine an extension from the URL path
	ext := srcExt

	// If no extension, try Content-Type header
	if ext == "" {
		if ct := resp.Header().Get("Content-Type"); ct != "" {
			// strip any charset
			ct = strings.Split(ct, ";")[0]
			if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 { // ignore errors; use the last extension only if one or more are returned
				ext = exts[len(exts)-1] // use the last extension (most specific)
			}
		}
	}
	// If still no extension, sniff the first bytes
	var bodyReader io.Reader = resp.Body()
	if ext == "" { // read first 512 bytes to sniff content type if extension not determined yet
		head := make([]byte, 512)
		n, rerr := io.ReadFull(resp.Body(), head)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			err = errs.Wrap(rerr, errs.WithContext("url", parsed.String()))
			return
		}
		if n > 0 {
			snip := head[:n] // actual bytes read
			if ct := http.DetectContentType(snip); ct != "" {
				if exts, _ := mime.ExtensionsByType(strings.Split(ct, ";")[0]); len(exts) > 0 { // ignore errors; use the last extension only if one or more are returned
					ext = exts[len(exts)-1] // use the last extension (most specific)
				}
			}
			// prepend the bytes back to the body reader
			bodyReader = io.MultiReader(bytes.NewReader(snip), resp.Body())
		}
	}
	// still no extension
	if ext == "" {
		ext = ".img"
	}

	var outF *os.File
	if temporary {
		// Create a temporary file
		var cerr error
		outF, cerr = createFile(true, destDir, "webinfo-image-*"+ext)
		if cerr != nil {
			err = errs.Wrap(cerr, errs.WithContext("dir", destDir), errs.WithContext("file", "temporary file"))
			return
		}
	} else {
		// Create a permanent file
		destPath := filepath.Join(destDir, srcFname)
		if len(srcExt) == 0 {
			destPath += ext
		}
		var cerr error
		outF, cerr = createFile(false, "", destPath)
		if cerr != nil {
			err = errs.Wrap(cerr, errs.WithContext("path", destPath))
			return
		}
	}
	defer func() {
		if cerr := outF.Close(); cerr != nil && cerr != os.ErrClosed {
			err = errs.Join(errs.Wrap(cerr, errs.WithContext("path", outF.Name())), err)
		}
	}()

	if _, cerr := io.Copy(outF, bodyReader); cerr != nil {
		err = errs.Wrap(cerr, errs.WithContext("path", outF.Name()))
		return
	}
	outPath = outF.Name()
	return
}

// DownloadThumbnail downloads the source image, resizes it to width while keeping
// aspect ratio, and writes the thumbnail to destDir.
//
// width defaults to 150 when width <= 0.
//
// The source image is downloaded to a temporary file first and removed on return.
// Output uses a temporary name when temporary is true; otherwise it uses
// "<base>-thumb<ext>" derived from the original image URL.
// Decoded WebP input is currently written as JPEG output (fallback behavior).
//
// Returned errors are wrapped with context and include cleanup failures.
func (w *Webinfo) DownloadThumbnail(ctx context.Context, destDir string, width int, temporary bool) (outPath string, err error) {
	if w == nil {
		err = errs.Wrap(ErrNullPointer)
		return
	}

	if width <= 0 {
		width = 150
	}

	// make directory if needed
	destDir = filepath.Clean(destDir)
	if len(destDir) > 0 {
		if derr := os.MkdirAll(destDir, 0o750); derr != nil {
			err = errs.Wrap(derr, errs.WithContext("destDir", destDir))
			return
		}
	}

	// Download original image to a temporary file (always temp to avoid name collisions)
	origPath, derr := w.DownloadImage(ctx, "", true)
	if derr != nil {
		err = errs.Wrap(derr, errs.WithContext("url", w.ImageURL))
		return
	}
	defer func() { // ensure original temp file removed when done
		if rerr := os.Remove(origPath); rerr != nil {
			err = errs.Join(errs.Wrap(rerr, errs.WithContext("path", origPath)), err)
		}
	}()

	// open original image
	f, oerr := os.Open(filepath.Clean(origPath))
	if oerr != nil {
		err = errs.Wrap(oerr, errs.WithContext("path", origPath))
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && cerr != os.ErrClosed {
			err = errs.Join(errs.Wrap(cerr, errs.WithContext("path", f.Name())), err)
		}
	}()

	img, format, derr := decodeImage(f)
	if derr != nil {
		err = errs.Wrap(derr, errs.WithContext("path", origPath))
		return
	}

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()
	if origW == 0 || origH == 0 {
		err = errs.Wrap(ErrNoImageURL, errs.WithContext("url", w.ImageURL))
		return
	}

	newH := int(math.Round(float64(width) * float64(origH) / float64(origW)))
	if newH <= 0 {
		newH = 1
	}

	thumb := image.NewRGBA(image.Rect(0, 0, width, newH))
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), img, bounds, draw.Over, nil) // scale by Catmull-Rom

	// determine extension/format for output
	var ext string
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		ext = ".jpg"
	case "png":
		ext = ".png"
	case "gif":
		ext = ".gif"
	case "webp": // webp is not supported by the standard library, but we can still use .jpg as a fallback
		ext = ".jpg"
		format = "jpeg"
	default:
		ext = ".png"
		format = "png"
	}

	var outF *os.File
	if temporary {
		// temporary: create temp file
		var cerr error
		outF, cerr = createFile(true, destDir, "webinfo-thumb-*"+ext)
		if cerr != nil {
			err = errs.Wrap(cerr, errs.WithContext("dir", destDir), errs.WithContext("file", "temporary thumbnail"))
			return
		}
	} else {
		// not temporary: build filename based on original URL basename
		base := "webinfo-image"
		if u, uerr := fetch.URL(strings.TrimSpace(w.ImageURL)); uerr == nil {
			bn := path.Base(u.Path)
			if bn != "" && bn != "." && bn != "/" {
				base = strings.TrimSuffix(bn, path.Ext(bn))
			}
		}
		destName := base + "-thumb" + ext
		destPath := filepath.Join(destDir, destName)

		var cerr error
		outF, cerr = createFile(false, "", destPath)
		if cerr != nil {
			err = errs.Wrap(cerr, errs.WithContext("path", destPath))
			return
		}
	}
	defer func() {
		if cerr := outF.Close(); cerr != nil && cerr != os.ErrClosed {
			err = errs.Join(errs.Wrap(cerr, errs.WithContext("path", outF.Name())), err)
		}
	}()

	if oerr := outputImage(outF, thumb, format); oerr != nil {
		err = errs.Wrap(oerr, errs.WithContext("path", outF.Name()))
		return
	}
	outPath = outF.Name()
	return
}

// outputImage writes src to dst using format-specific encoders.
// Tests may replace this variable.
var outputImage = func(dst *os.File, src *image.RGBA, format string) error {
	switch format {
	case "jpeg", "jpg":
		return jpeg.Encode(dst, src, &jpeg.Options{Quality: 90})
	case "png":
		return png.Encode(dst, src)
	case "gif":
		return gif.Encode(dst, src, nil)
	}
	return png.Encode(dst, src) // default to PNG
}

// createFile creates temporary or permanent files.
// Tests may replace this variable.
var createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
	if temp {
		return os.CreateTemp(dir, pathOrPattern)
	}
	return os.Create(filepath.Clean(pathOrPattern))
}

// decodeImage wraps image.Decode.
// Tests may replace this variable.
var decodeImage = func(r io.Reader) (image.Image, string, error) {
	return image.Decode(r)
}

// newHTTPClient returns the default HTTP client used by downloads.
// Tests may replace this variable.
var newHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

/* Copyright 2025-2026 Spiegel
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * 	http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
