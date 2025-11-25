package webinfo

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// helper to create a simple filled PNG
func makePNGBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// fill with a solid color
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

// helper to create a simple filled JPEG
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
