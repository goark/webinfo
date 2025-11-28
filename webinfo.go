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

	"github.com/goark/errs"
	"github.com/goark/fetch"
	"golang.org/x/image/draw"
)

// Webinfo holds common metadata extracted from a web page.
// It captures information useful for previews or link metadata:
//
// - URL: the original page URL.
// - Location: the location declared by the page (if any).
// - Canonical: the canonical URL declared by the page (if any).
// - Title: the page title.
// - Description: a short summary or meta description for the page.
// - ImageURL: a representative image URL suitable for previews.
// - UserAgent: the User-Agent string used to fetch the page.
//
// Fields may be empty or nil when the corresponding metadata is not present.
type Webinfo struct {
	URL         string `json:"url,omitempty"`         // Original page URL
	Location    string `json:"location,omitempty"`    // Location
	Canonical   string `json:"canonical,omitempty"`   // Canonical URL
	Title       string `json:"title,omitempty"`       // Page title
	Description string `json:"description,omitempty"` // Meta description
	ImageURL    string `json:"image_url,omitempty"`   // Representative image URL
	UserAgent   string `json:"user_agent,omitempty"`  // User-Agent used to fetch the page
}

// DownloadImage downloads the image pointed to by w.ImageURL and saves it to destDir,
// returning the path of the saved file (outPath) or an error.
//
// Behavior:
//   - The method is a receiver on *Webinfo and will return an error if w is nil or if
//     ImageURL is empty.
//   - ctx is used to control/cancel the underlying HTTP request.
//   - destDir is cleaned with filepath.Clean. If it is non-empty, the directory (and any
//     required parents) will be created with mode 0750. If destDir is empty, file
//     creation uses the system/default behavior for temporary or current directories.
//   - If `temporary` is true, the image is written to a temporary file (created via
//     the package-level `createFile` helper which wraps `os.CreateTemp`) and the
//     temporary file path is returned. If the URL path does not contain a filename,
//     `temporary` is forced true.
//   - If `temporary` is false, the image is written to `destDir` with the filename
//     taken from the URL path. If the URL filename has no extension, an extension is
//     appended (see extension resolution below). Existing files will be truncated by
//     the underlying `createFile`/`os.Create` behavior.
//
// HTTP download and content-type/extension resolution:
//   - The image is fetched using an HTTP GET performed with the provided context; the
//     request User-Agent is set via getUserAgent(w.UserAgent).
//   - Extension resolution order:
//     1) Extension from the URL path (if present).
//     2) Extension(s) derived from the Content-Type response header via mime.ExtensionsByType.
//     3) If still unknown, the first up-to-512 bytes of the body are read and
//     http.DetectContentType is used to guess the content type, then mime.ExtensionsByType.
//     4) If no extension can be determined, ".img" is used as a fallback.
//   - When bytes are sniffed from the body, they are prepended back to the reader so the
//     full image is written to disk. When multiple extensions are returned by
//     mime.ExtensionsByType the implementation picks the last returned extension.
//   - File creation is performed via the package-level `createFile` variable which tests
//     may override to simulate create failures.
//
// Resource management and errors:
//   - The response body and any created file are closed using deferred cleanup; any close
//     errors are joined into the returned error.
//   - I/O, network and OS errors are returned (wrapped with contextual information).
//   - On success, outPath contains the absolute/relative path to the saved image file;
//     on error, outPath will be empty and err will describe the failure.
//
// Notes:
//   - The function may truncate an existing destination file with the same name.
//   - The exact behavior of temporary file placement when destDir is empty follows the
//     semantics of os.CreateTemp.
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

// DownloadThumbnail downloads the image referenced by the Webinfo receiver, scales it
// to the requested width (preserving aspect ratio), and writes the resulting thumbnail
// image to disk.
//
// The method returns the path to the created thumbnail file or an error. Behavior details:
//   - If the receiver is nil, ErrNullPointer is returned.
//   - If width <= 0, a default width of 150 pixels is used.
//   - destDir is cleaned and, if non-empty, created with mode 0750 (os.MkdirAll).
//   - The original image is always downloaded to a temporary file via DownloadImage(..., true).
//     That temporary original file is removed when the function returns (even on error).
//   - The original image file is opened and decoded. If decoding fails, an error is returned.
//   - The thumbnail height is computed to preserve aspect ratio: newH = round(width * origH / origW).
//     newH is clamped to at least 1 pixel.
//   - The image is resized using a Catmull-Rom resampler into an RGBA image of size
//     width x newH.
//   - The output format/extension is chosen from the decoded format: jpeg/jpg → .jpg, png → .png,
//     gif → .gif. Unknown formats fall back to PNG.
//   - If `temporary` is true, the thumbnail file is created via the package-level
//     `createFile` helper (which wraps `os.CreateTemp`) in `destDir` using the
//     pattern "webinfo-thumb-*<ext>"; the temporary file path is returned.
//   - If `temporary` is false, the output filename is derived from the original image
//     URL basename (falling back to "webinfo-image") and named "<base>-thumb<ext>" in
//     `destDir`.
//   - The encoder used to write the thumbnail is the package-level `outputImage` function
//     variable; tests may replace this variable to simulate encoder failures. The image
//     decoding step uses the package-level `decodeImage` wrapper around `image.Decode`,
//     which tests may also override.
//   - Files are properly closed with deferred cleanup; any close/remove errors are joined into
//     the returned error using the errs package.
//   - All filesystem, download, and image-processing errors are wrapped with contextual
//     information (e.g., paths, URL) before being returned.
//
// Parameters:
//   - ctx: context for cancellation and timeouts passed to DownloadImage and other operations.
//   - destDir: destination directory for the thumbnail (cleaned). If empty, creation uses the
//     current directory semantics of os.Create/os.CreateTemp.
//   - width: desired thumbnail width in pixels (defaults to 150 if <= 0).
//   - temporary: if true, create a uniquely-named temporary file; otherwise create a stable
//     filename based on the original image basename.
//
// Returns:
//   - outPath: filesystem path to the created thumbnail file (valid when err == nil).
//   - err: non-nil on failure; common failure reasons include download errors, decode errors,
//     filesystem errors, and invalid image dimensions (ErrNoImageURL).
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

// outputImage encodes the provided *image.RGBA src and writes it to dst using
// the encoder corresponding to the given format string. It is a variable so
// tests can replace it to simulate encoder failures.
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

// createFile is a package-level helper used to create files. It abstracts
// the creation of temporary and permanent files so tests can replace it to
// simulate failures during os.Create/os.CreateTemp.
var createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
	if temp {
		return os.CreateTemp(dir, pathOrPattern)
	}
	return os.Create(filepath.Clean(pathOrPattern))
}

// decodeImage is a package-level wrapper around image.Decode so tests can
// replace it to simulate decoding behaviors (e.g., returning zero-dimension
// images) without modifying stdlib functions.
var decodeImage = func(r io.Reader) (image.Image, string, error) {
	return image.Decode(r)
}

// newHTTPClient returns the http.Client used for web requests. It is a package-level
// variable so tests can override it. By default it sets a 30-second timeout for
// the whole request (connect+read+write).
var newHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

/* Copyright 2025 Spiegel
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
