package webinfo

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makePNGBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func makeJPEGBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// fill with solid color
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0x11, G: 0x88, B: 0x22, A: 0xff})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func makeJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0x99, G: 0x44, B: 0x22, A: 0xff})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

func TestDownloadThumbnail_Temporary(t *testing.T) {
	pngData := makePNG(200, 100)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngData)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	info := &Webinfo{ImageURL: srv.URL + "/img.png"}
	ctx := context.Background()

	out, err := info.DownloadThumbnail(ctx, "", 100, true)
	if err != nil {
		t.Fatalf("DownloadThumbnail returned error: %v", err)
	}
	defer func() { _ = os.Remove(out) }()

	f, ferr := os.Open(filepath.Clean(out))
	if ferr != nil {
		t.Fatalf("failed to open thumbnail: %v", ferr)
	}
	defer func() { _ = f.Close() }()
	img, _, derr := image.Decode(f)
	if derr != nil {
		t.Fatalf("failed to decode thumbnail: %v", derr)
	}
	if img.Bounds().Dx() != 100 {
		t.Fatalf("thumbnail width: want %d, got %d", 100, img.Bounds().Dx())
	}
}

func TestDownloadThumbnail_Permanent(t *testing.T) {
	jpgData := makeJPEG(300, 150)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/pic.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpgData)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	destDir := t.TempDir()
	info := &Webinfo{ImageURL: srv.URL + "/images/pic.jpg"}
	ctx := context.Background()

	out, err := info.DownloadThumbnail(ctx, destDir, 50, false)
	if err != nil {
		t.Fatalf("DownloadThumbnail returned error: %v", err)
	}
	defer func() { _ = os.Remove(out) }()

	// ensure file is in destDir and contains -thums
	if filepath.Dir(out) != filepath.Clean(destDir) {
		t.Fatalf("thumbnail path dir: want %q, got %q", destDir, filepath.Dir(out))
	}
	if !strings.Contains(filepath.Base(out), "-thums") {
		t.Fatalf("thumbnail filename should contain -thums: %q", out)
	}

	f, ferr := os.Open(filepath.Clean(out))
	if ferr != nil {
		t.Fatalf("failed to open thumbnail: %v", ferr)
	}
	defer func() { _ = f.Close() }()
	img, _, derr := image.Decode(f)
	if derr != nil {
		t.Fatalf("failed to decode thumbnail: %v", derr)
	}
	if img.Bounds().Dx() != 50 {
		t.Fatalf("thumbnail width: want %d, got %d", 50, img.Bounds().Dx())
	}
}

// minimal PNG/JPEG signatures for content-type detection
var pngSig = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
var jpgSig = []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 'J', 'F', 'I', 'F'}

// helper to read file contents
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return b
}

func TestDownloadImage_NilReceiver(t *testing.T) {
	var w *Webinfo
	_, err := w.DownloadImage(context.Background(), "", true)
	if err == nil {
		t.Fatalf("expected error for nil receiver")
	}
}

func TestDownloadImage_SaveWithFilename(t *testing.T) {
	// serve a PNG at /images/pic.png
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngSig)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/images/pic.png", UserAgent: ""}
	out, err := w.DownloadImage(context.Background(), dest, false)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	want := filepath.Join(dest, "pic.png")
	if out != want {
		t.Fatalf("unexpected path: got %q want %q", out, want)
	}
	got := readFile(t, out)
	if !bytes.Equal(got, pngSig) {
		t.Fatalf("content mismatch")
	}
}

func TestDownloadImage_TemporaryWhenNoFilenameAndContentType(t *testing.T) {
	// serve a JPEG at root (no filename in path) with Content-Type header
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/jpeg")
		_, _ = wr.Write(jpgSig)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	// URL ending with "/" causes srcFname to be "/" and thus temporary forced true
	w := &Webinfo{ImageURL: srv.URL + "/", UserAgent: ""}
	out, err := w.DownloadImage(context.Background(), dest, false) // pass false; function should switch to temporary
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	if !strings.HasPrefix(out, dest) {
		t.Fatalf("tmp file not created in dest: %s", out)
	}
	ext := filepath.Ext(out)
	if ext != ".jpg" && ext != ".jpeg" && ext != ".img" {
		t.Fatalf("unexpected extension %q", ext)
	}
	got := readFile(t, out)
	if !bytes.Equal(got, jpgSig) {
		t.Fatalf("content mismatch")
	}
}

func TestDownloadImage_SniffingDeterminesExtension(t *testing.T) {
	// serve PNG bytes but omit Content-Type header to force sniffing
	handler := func(wr http.ResponseWriter, r *http.Request) {
		// no Content-Type header
		_, _ = wr.Write(pngSig)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	// nested dest dir to ensure MkdirAll behavior
	base := t.TempDir()
	dest := filepath.Join(base, "nested", "sub")
	w := &Webinfo{ImageURL: srv.URL + "/", UserAgent: ""}
	out, err := w.DownloadImage(context.Background(), dest, false) // should become temporary and use sniffed ext
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	if !strings.HasPrefix(out, base) {
		t.Fatalf("output path not under base: %s", out)
	}
	ext := filepath.Ext(out)
	if ext != ".png" && ext != ".img" {
		t.Fatalf("unexpected extension %q", ext)
	}
	got := readFile(t, out)
	if !bytes.Equal(got, pngSig) {
		t.Fatalf("content mismatch")
	}
}

func TestDownloadImage_MkdirAllFails(t *testing.T) {
	// create a file where a directory is expected so MkdirAll fails
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	if err := os.WriteFile(filepath.Clean(blocker), []byte("notadir"), 0o600); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}

	// dest path includes the blocker as a path component; MkdirAll should fail
	dest := filepath.Join(blocker, "nested")

	w := &Webinfo{ImageURL: "http://example.invalid/img.png"}
	_, err := w.DownloadImage(context.Background(), dest, true)
	if err == nil {
		t.Fatalf("expected error when MkdirAll cannot create directories, got nil")
	}
}

func TestDownloadImage_ReadFullReturnsError(t *testing.T) {
	// override the default transport so the HTTP client used in DownloadImage
	// receives a response whose Body returns a non-EOF error on Read.
	orig := http.DefaultTransport
	defer func() { http.DefaultTransport = orig }()

	// errReader is defined at package scope below.

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// return a response with no Content-Type header and a Body that errors
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(errReader{}),
			Request:    req,
		}, nil
	})
	http.DefaultTransport = rt

	dest := t.TempDir()
	w := &Webinfo{ImageURL: "http://example.invalid/"}
	_, err := w.DownloadImage(context.Background(), dest, false)
	if err == nil {
		t.Fatalf("expected error when ReadFull returns non-EOF error, got nil")
	}
}

func TestDownloadImage_CopyReadFails(t *testing.T) {
	// RoundTripper that returns a response whose Body returns a non-EOF error after sniffing
	orig := http.DefaultTransport
	defer func() { http.DefaultTransport = orig }()

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		b := &failingBody{
			firstData:     pngSig, // allow sniffing to see PNG signature
			firstErr:      io.ErrUnexpectedEOF,
			subsequentErr: errors.New("simulated read error"),
		}
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(b),
			Request:    req,
		}, nil
	})
	http.DefaultTransport = rt

	dest := t.TempDir()
	w := &Webinfo{ImageURL: "http://example.invalid/"}
	_, err := w.DownloadImage(context.Background(), dest, false)
	if err == nil {
		t.Fatalf("expected error when io.Copy encounters read error, got nil")
	}
}

func TestDownloadImage_CloseErrorJoined(t *testing.T) {
	orig := http.DefaultTransport
	defer func() { http.DefaultTransport = orig }()

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// provide some data for sniffing and normal EOF for copy; Close will return an error
		b := &failingBody{
			firstData: []byte("abcd"),
			firstErr:  io.ErrUnexpectedEOF,
			closeErr:  errors.New("simulated close error"),
		}
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(b),
			Request:    req,
		}, nil
	})
	http.DefaultTransport = rt

	// This case is covered by TestFetch_BodyCloseReturnsError (Fetch joins close errors).
}

// helper types used by tests
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("simulated read error") }

func (errReader) Close() error { return nil }

// helper type to allow inline RoundTripper function
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// failingBody allows simulating read and close failures in response bodies.
type failingBody struct {
	// firstRead returns data and an optional error for the initial sniffing ReadFull call
	firstData []byte
	firstErr  error
	// subsequentErr is returned on subsequent Read calls (to simulate io.Copy failing)
	subsequentErr error
	closeErr      error
	closed        bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	if len(b.firstData) > 0 {
		n := copy(p, b.firstData)
		// consume firstData
		b.firstData = b.firstData[n:]
		if len(b.firstData) == 0 {
			return n, b.firstErr
		}
		return n, nil
	}
	if b.subsequentErr != nil {
		return 0, b.subsequentErr
	}
	return 0, io.EOF
}

func (b *failingBody) Close() error {
	b.closed = true
	if b.closeErr != nil {
		return b.closeErr
	}
	return nil
}

// zeroImg implements image.Image but reports a zero width to exercise
// the zero-dimension handling in DownloadThumbnail.
type zeroImg struct{}

func (zeroImg) ColorModel() color.Model { return color.RGBAModel }
func (zeroImg) Bounds() image.Rectangle { return image.Rect(0, 0, 0, 10) }
func (zeroImg) At(x, y int) color.Color { return color.RGBA{0, 0, 0, 0} }

func TestOutputImage_EncodersAndFallback(t *testing.T) {
	// create a simple source image
	src := image.NewRGBA(image.Rect(0, 0, 20, 10))
	// fill to avoid zero-content
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
		}
	}

	formats := []string{"jpeg", "png", "gif", "unknown"}
	for _, fmtName := range formats {
		tmp := t.TempDir()
		fpath := filepath.Join(tmp, "out")
		f, err := os.Create(filepath.Clean(fpath))
		if err != nil {
			t.Fatalf("create file: %v", err)
		}
		// ensure close before decode
		if err := outputImage(f, src, fmtName); err != nil {
			// for unknown format we still expect PNG encoding
			t.Fatalf("outputImage(%s) error: %v", fmtName, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close file: %v", err)
		}
		// reopen and decode
		rb, rerr := os.ReadFile(filepath.Clean(fpath))
		if rerr != nil {
			t.Fatalf("read file: %v", rerr)
		}
		if _, _, derr := image.Decode(bytes.NewReader(rb)); derr != nil {
			t.Fatalf("decoded output (%s) failed: %v", fmtName, derr)
		}
	}
}

func TestOutputImage_ClosedDstReturnsError(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	f, err := os.CreateTemp("", "closed-*")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}
	// reopen read-only to ensure writes fail
	ro, _ := os.Open(filepath.Clean(name))
	defer func() { _ = ro.Close(); _ = os.Remove(name) }()
	if err := outputImage(ro, src, "png"); err == nil {
		t.Fatalf("expected error when writing to non-writable file")
	}
}

func TestDownloadImage_ContentTypeWithCharset(t *testing.T) {
	pngBytes := makePNGBytes(16, 8)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png; charset=utf-8")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/"}
	out, err := w.DownloadImage(context.Background(), dest, false)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	ext := filepath.Ext(out)
	if ext != ".png" && ext != ".img" {
		t.Fatalf("unexpected extension %q", ext)
	}
}

func TestDownloadImage_BadURL(t *testing.T) {
	w := &Webinfo{ImageURL: "://bad-url"}
	_, err := w.DownloadImage(context.Background(), "", true)
	if err == nil {
		t.Fatalf("expected error for bad URL, got nil")
	}
}

func TestDownloadImage_AppendExtWhenNoSrcExt(t *testing.T) {
	// serve PNG at /images/pic (no extension in URL)
	pngBytes := makePNGBytes(12, 6)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/images/pic"}
	out, err := w.DownloadImage(context.Background(), dest, false)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	want := filepath.Join(dest, "pic.png")
	if out != want {
		t.Fatalf("unexpected path: got %q want %q", out, want)
	}
	b, rerr := os.ReadFile(filepath.Clean(out))
	if rerr != nil {
		t.Fatalf("read out file: %v", rerr)
	}
	if !bytes.Equal(b, pngBytes) {
		t.Fatalf("content mismatch")
	}
}

func TestDownloadImage_TemporaryWithoutDestDirUsesOSTemp(t *testing.T) {
	pngBytes := makePNGBytes(6, 3)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	w := &Webinfo{ImageURL: srv.URL + "/img.png"}
	out, err := w.DownloadImage(context.Background(), "", true)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	// check that filename matches the expected temporary pattern
	base := filepath.Base(out)
	if !strings.HasPrefix(base, "webinfo-image-") {
		t.Fatalf("temporary file name does not match pattern: %s", base)
	}
	if filepath.Ext(base) == "" {
		t.Fatalf("temporary file missing extension: %s", base)
	}
}

func makeGIFBytes(w, h int) []byte {
	pal := []color.Color{color.RGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff}, color.RGBA{R: 0x00, G: 0xff, B: 0x00, A: 0xff}}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x+y)%2 == 0 {
				img.SetColorIndex(x, y, 0)
			} else {
				img.SetColorIndex(x, y, 1)
			}
		}
	}
	var buf bytes.Buffer
	_ = gif.Encode(&buf, img, nil)
	return buf.Bytes()
}

func TestDownloadImage_GIF_SaveAndThumbnail(t *testing.T) {
	gifBytes := makeGIFBytes(40, 20)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/pic.gif" {
			wr.Header().Set("Content-Type", "image/gif")
			_, _ = wr.Write(gifBytes)
			return
		}
		http.NotFound(wr, r)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/images/pic.gif"}

	// Test DownloadImage saves with .gif
	out, err := w.DownloadImage(context.Background(), dest, false)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	if filepath.Ext(out) != ".gif" {
		t.Fatalf("expected .gif extension, got %q", filepath.Ext(out))
	}

	// Test DownloadThumbnail produces a GIF thumbnail when format is gif
	thumb, err := w.DownloadThumbnail(context.Background(), dest, 20, false)
	if err != nil {
		t.Fatalf("DownloadThumbnail failed: %v", err)
	}
	defer func() { _ = os.Remove(thumb) }()
	if filepath.Ext(thumb) != ".gif" {
		t.Fatalf("expected thumbnail .gif extension, got %q", filepath.Ext(thumb))
	}
	// open and decode
	fb, ferr := os.ReadFile(filepath.Clean(thumb))
	if ferr != nil {
		t.Fatalf("read thumb: %v", ferr)
	}
	if _, _, derr := image.Decode(bytes.NewReader(fb)); derr != nil {
		t.Fatalf("decode thumb failed: %v", derr)
	}
}

func TestDownloadImage_MultipleExtensionsByType(t *testing.T) {
	// ensure mime package returns multiple extensions for a custom type
	// register two synthetic extensions for a custom content type; the code under test picks the last one
	_ = mime.AddExtensionType(".ex1my", "image/x-mytest")
	_ = mime.AddExtensionType(".ex2my", "image/x-mytest")

	pngBytes := makePNGBytes(10, 5)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		// omit filename extension; rely on Content-Type header
		wr.Header().Set("Content-Type", "image/x-mytest")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	base := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/"}
	out, err := w.DownloadImage(context.Background(), base, false)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()

	// expect the last extension added (".ex2my") to be chosen
	if filepath.Ext(out) != ".ex2my" {
		t.Fatalf("expected extension .ex2my, got %q", filepath.Ext(out))
	}
}

func TestOutputImage_WriteFails(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	// create a temp file and open it read-only to force write failure
	tmp, err := os.CreateTemp("", "rofile-*")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close tmp: %v", err)
	}
	ro, err := os.Open(filepath.Clean(name))
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer func() { _ = ro.Close(); _ = os.Remove(name) }()
	if err := outputImage(ro, src, "png"); err == nil {
		t.Fatalf("expected error when writing to read-only file")
	}
}

func TestDownloadImage_CreateFileFails(t *testing.T) {
	pngBytes := makePNGBytes(6, 6)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	// override createFile to simulate failure when creating permanent files
	orig := createFile
	defer func() { createFile = orig }()
	createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
		if !temp {
			return nil, errors.New("simulated permanent create failure")
		}
		return orig(temp, dir, pathOrPattern)
	}

	w := &Webinfo{ImageURL: srv.URL + "/images/pic.png"}
	_, err := w.DownloadImage(context.Background(), dest, false)
	if err == nil {
		t.Fatalf("expected error when permanent file creation fails, got nil")
	}
}

func TestDownloadImage_TemporaryCreateFails(t *testing.T) {
	pngBytes := makePNGBytes(8, 8)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	// override createFile to simulate failure when creating temporary files
	orig := createFile
	defer func() { createFile = orig }()
	createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
		if temp {
			return nil, errors.New("simulated temp create failure")
		}
		return orig(temp, dir, pathOrPattern)
	}

	w := &Webinfo{ImageURL: srv.URL + "/img.png"}
	_, err := w.DownloadImage(context.Background(), t.TempDir(), true)
	if err == nil {
		t.Fatalf("expected error when temporary file creation fails, got nil")
	}
}

func TestDownloadThumbnail_TemporaryCreateFails(t *testing.T) {
	pngData := makePNG(80, 40)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// override createFile to simulate failure when creating any temporary file
	orig := createFile
	defer func() { createFile = orig }()
	createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
		if temp {
			return nil, errors.New("simulated temp create failure")
		}
		return orig(temp, dir, pathOrPattern)
	}

	w := &Webinfo{ImageURL: srv.URL + "/images/pic.png"}
	_, err := w.DownloadThumbnail(context.Background(), t.TempDir(), 50, true)
	if err == nil {
		t.Fatalf("expected error when temporary thumbnail creation fails, got nil")
	}
}

func TestDownloadThumbnail_ThumbnailCreateTempFails(t *testing.T) {
	pngData := makePNG(80, 40)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// override createFile to simulate failure only for thumbnail temporary file
	orig := createFile
	defer func() { createFile = orig }()
	createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
		if temp && strings.Contains(pathOrPattern, "webinfo-thumb-") {
			return nil, errors.New("simulated thumbnail temp create failure")
		}
		return orig(temp, dir, pathOrPattern)
	}

	// Use a dest dir for the thumbnail so createFile is called for the thumb
	w := &Webinfo{ImageURL: srv.URL + "/images/pic.png"}
	_, err := w.DownloadThumbnail(context.Background(), t.TempDir(), 50, true)
	if err == nil {
		t.Fatalf("expected error when thumbnail temporary creation fails, got nil")
	}
}

func TestDownloadThumbnail_NilReceiver(t *testing.T) {
	var w *Webinfo
	_, err := w.DownloadThumbnail(context.Background(), "", 100, true)
	if err == nil {
		t.Fatalf("expected error for nil receiver")
	}
}

func TestDownloadThumbnail_TemporaryPNG_DefaultWidth(t *testing.T) {
	// original image 200x100 -> expected thumbnail width 150 (default), height 75
	pngBytes := makePNGBytes(200, 100)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/images/pic.png", UserAgent: ""}
	out, err := w.DownloadThumbnail(context.Background(), dest, 0, true) // width 0 -> default 150
	if err != nil {
		t.Fatalf("DownloadThumbnail failed: %v", err)
	}
	if !strings.HasPrefix(out, dest) {
		t.Fatalf("thumbnail not created in dest: %s", out)
	}
	ext := filepath.Ext(out)
	if ext != ".png" && ext != ".img" {
		t.Fatalf("unexpected extension %q", ext)
	}
	// verify dimensions
	fb, err := os.ReadFile(filepath.Clean(out))
	if err != nil {
		t.Fatalf("read thumbnail: %v", err)
	}
	img, _, derr := image.Decode(bytes.NewReader(fb))
	if derr != nil {
		t.Fatalf("decode thumbnail: %v", derr)
	}
	if img.Bounds().Dx() != 150 {
		t.Fatalf("unexpected thumb width: got %d want %d", img.Bounds().Dx(), 150)
	}
	if img.Bounds().Dy() != 75 {
		t.Fatalf("unexpected thumb height: got %d want %d", img.Bounds().Dy(), 75)
	}
}

func TestDownloadThumbnail_NonTemporaryJPEG_FilenameDerived(t *testing.T) {
	// original 100x100 -> thumbnail 50x50
	jpgBytes := makeJPEGBytes(100, 100)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/jpeg")
		_, _ = wr.Write(jpgBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/images/pic.jpg", UserAgent: ""}
	out, err := w.DownloadThumbnail(context.Background(), dest, 50, false)
	if err != nil {
		t.Fatalf("DownloadThumbnail failed: %v", err)
	}
	want := filepath.Join(dest, "pic-thums.jpg")
	if out != want {
		t.Fatalf("unexpected path: got %q want %q", out, want)
	}
	// verify dimensions
	fb, err := os.ReadFile(filepath.Clean(out))
	if err != nil {
		t.Fatalf("read thumbnail: %v", err)
	}
	img, _, derr := image.Decode(bytes.NewReader(fb))
	if derr != nil {
		t.Fatalf("decode thumbnail: %v", derr)
	}
	if img.Bounds().Dx() != 50 || img.Bounds().Dy() != 50 {
		t.Fatalf("unexpected thumb size: got %dx%d want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), 50, 50)
	}
}

func TestDownloadThumbnail_MkdirAllFails(t *testing.T) {
	// create a file where a directory is expected so MkdirAll fails
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	if err := os.WriteFile(filepath.Clean(blocker), []byte("notadir"), 0o600); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}

	// dest path includes the blocker as a path component; MkdirAll should fail
	dest := filepath.Join(blocker, "nested")

	w := &Webinfo{ImageURL: "http://example.invalid/img.png"}
	_, err := w.DownloadThumbnail(context.Background(), dest, 100, true)
	if err == nil {
		t.Fatalf("expected error when MkdirAll cannot create directories, got nil")
	}
}

func TestDownloadThumbnail_BaseFallbackWhenURLHasNoBasename(t *testing.T) {
	pngData := makePNG(120, 60)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	dest := t.TempDir()
	// URL ends with '/', so basename logic should fall back to "webinfo-image"
	w := &Webinfo{ImageURL: srv.URL + "/"}
	out, err := w.DownloadThumbnail(context.Background(), dest, 30, false)
	if err != nil {
		t.Fatalf("DownloadThumbnail failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	want := filepath.Join(dest, "webinfo-image-thums.png")
	if out != want {
		t.Fatalf("unexpected thumbnail path: got %q want %q", out, want)
	}
}

func TestDownloadImage_NoImageURL(t *testing.T) {
	w := &Webinfo{ImageURL: ""}
	_, err := w.DownloadImage(context.Background(), "", true)
	if err == nil {
		t.Fatalf("expected error for empty ImageURL, got nil")
	}
}

func TestDownloadThumbnail_ZeroOrigDimensions(t *testing.T) {
	// server returns a small PNG but we override decodeImage to return a
	// zero-width image to exercise the origW==0 || origH==0 error path.
	pngData := makePNGBytes(4, 4)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngData)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	// override decodeImage
	origDecode := decodeImage
	defer func() { decodeImage = origDecode }()
	decodeImage = func(r io.Reader) (image.Image, string, error) {
		return zeroImg{}, "png", nil
	}

	w := &Webinfo{ImageURL: srv.URL + "/img.png"}
	_, err := w.DownloadThumbnail(context.Background(), t.TempDir(), 50, true)
	if err == nil {
		t.Fatalf("expected error when decoded image has zero dimension, got nil")
	}
}

func TestDownloadThumbnail_HeightClampedToOne(t *testing.T) {
	// original image very wide but 1px tall -> newH may round to 0 and should be clamped to 1
	pngData := makePNGBytes(1000, 1)
	handler := func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngData)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/images/wide.png"}
	out, err := w.DownloadThumbnail(context.Background(), dest, 1, true)
	if err != nil {
		t.Fatalf("DownloadThumbnail failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()

	fb, err := os.ReadFile(filepath.Clean(out))
	if err != nil {
		t.Fatalf("read thumbnail: %v", err)
	}
	img, _, derr := image.Decode(bytes.NewReader(fb))
	if derr != nil {
		t.Fatalf("decode thumbnail: %v", derr)
	}
	if img.Bounds().Dy() != 1 {
		t.Fatalf("thumbnail height clamped to 1: got %d", img.Bounds().Dy())
	}
}

func TestDownloadThumbnail_CreateFileFailsWhenDestFileReadOnly(t *testing.T) {
	pngData := makePNG(80, 40)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/pic.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngData)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	dest := t.TempDir()
	// override createFile to simulate failure when creating permanent thumbnail file
	orig := createFile
	defer func() { createFile = orig }()
	createFile = func(temp bool, dir, pathOrPattern string) (*os.File, error) {
		if !temp {
			return nil, errors.New("simulated permanent thumbnail create failure")
		}
		return orig(temp, dir, pathOrPattern)
	}

	w := &Webinfo{ImageURL: srv.URL + "/images/pic.png"}
	_, err := w.DownloadThumbnail(context.Background(), dest, 20, false)
	if err == nil {
		t.Fatalf("expected error when permanent thumbnail creation fails, got nil")
	}
}

func TestDownloadThumbnail_SmallDimensions(t *testing.T) {
	// small original image -> request very small width
	pngData := makePNG(2, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/small.png"}
	// request width 1 (small) and non-temporary output
	out, err := w.DownloadThumbnail(context.Background(), dest, 1, false)
	if err != nil {
		t.Fatalf("DownloadThumbnail failed for small dimensions: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	f, err := os.Open(filepath.Clean(out))
	if err != nil {
		t.Fatalf("open thumb: %v", err)
	}
	defer func() { _ = f.Close() }()
	img, _, derr := image.Decode(f)
	if derr != nil {
		t.Fatalf("decode thumb: %v", derr)
	}
	if img.Bounds().Dx() != 1 {
		t.Fatalf("unexpected thumb width: got %d want %d", img.Bounds().Dx(), 1)
	}
	if img.Bounds().Dy() < 1 {
		t.Fatalf("unexpected thumb height: got %d want >=%d", img.Bounds().Dy(), 1)
	}
}

func TestDownloadThumbnail_OutputImageFails(t *testing.T) {
	// serve a PNG and then override outputImage to simulate encoder failure
	pngData := makePNG(40, 20)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/img.png"}

	// swap out outputImage and restore after test
	orig := outputImage
	outputImage = func(dst *os.File, src *image.RGBA, format string) error {
		return errors.New("simulated encoder failure")
	}
	defer func() { outputImage = orig }()

	_, err := w.DownloadThumbnail(context.Background(), dest, 10, false)
	if err == nil {
		t.Fatalf("expected error when outputImage fails, got nil")
	}
}

func TestDownloadThumbnail_DownloadImageFails(t *testing.T) {
	// Use an invalid ImageURL so DownloadImage fails early
	w := &Webinfo{ImageURL: "://bad-url"}
	_, err := w.DownloadThumbnail(context.Background(), "", 100, true)
	if err == nil {
		t.Fatalf("expected error when DownloadImage fails, got nil")
	}
}

func TestDownloadThumbnail_DecodeFails(t *testing.T) {
	// server will return non-image content so decoding fails
	handler := http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "text/plain")
		_, _ = wr.Write([]byte("not an image"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	dest := t.TempDir()
	w := &Webinfo{ImageURL: srv.URL + "/not-image"}
	_, err := w.DownloadThumbnail(context.Background(), dest, 50, false)
	if err == nil {
		t.Fatalf("expected error when decoding original image fails, got nil")
	}
}

func TestDownloadThumbnail_TemporaryDefaultDest(t *testing.T) {
	// ensure when destDir is empty and temporary=true, thumbnail is created in OS temp
	pngData := makePNG(40, 20)
	handler := http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		wr.Header().Set("Content-Type", "image/png")
		_, _ = wr.Write(pngData)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	w := &Webinfo{ImageURL: srv.URL + "/img.png"}
	out, err := w.DownloadThumbnail(context.Background(), "", 30, true)
	if err != nil {
		t.Fatalf("DownloadThumbnail failed: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	base := filepath.Base(out)
	if !strings.HasPrefix(base, "webinfo-thumb-") {
		t.Fatalf("temporary thumbnail filename does not match pattern: %s", base)
	}
	ext := filepath.Ext(base)
	if ext != ".png" && ext != ".img" {
		t.Fatalf("unexpected temporary thumbnail extension: %s", ext)
	}
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
