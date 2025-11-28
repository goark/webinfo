package webinfo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func TestFetch_ParseMeta(t *testing.T) {
	// Handler returns HTML with multiple meta tags to test precedence/overrides.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
			<html><head>
			<title>Original Title</title>
			<meta name="description" content="NameDesc">
			<meta property="twitter:description" content="TwitterDesc">
			<meta property="og:description" content="OGDesc">
			<meta property="twitter:title" content="TwitterTitle">
			<meta property="og:title" content="OGTitle">
			<meta property="twitter:image" content="twitter.jpg">
			<meta property="og:image" content="og.jpg">
			<link rel="canonical" href="https://example.com/canonical">
			</head><body>Hello</body></html>`))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	info, err := Fetch(ctx, srv.URL, "")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if info.Title != "OGTitle" {
		t.Errorf("Title: want %q, got %q", "OGTitle", info.Title)
	}
	if info.Description != "OGDesc" {
		t.Errorf("Description: want %q, got %q", "OGDesc", info.Description)
	}
	if info.ImageURL != "og.jpg" {
		t.Errorf("ImageURL: want %q, got %q", "og.jpg", info.ImageURL)
	}
	if info.Canonical != "https://example.com/canonical" {
		t.Errorf("Canonical: want %q, got %q", "https://example.com/canonical", info.Canonical)
	}
}

func TestFetch_DefaultUserAgent(t *testing.T) {
	uaCh := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uaCh <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>X</title></head><body></body></html>"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	info, err := Fetch(ctx, srv.URL, "")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	var gotUA string
	select {
	case gotUA = <-uaCh:
	case <-time.After(time.Second):
		t.Fatalf("server did not receive request")
	}
	wantUA := "Mozilla/5.0 (Windows NT 6.1; rv:11.0) Gecko/20100101 Firefox/11.0"
	if gotUA != wantUA {
		t.Errorf("User-Agent: want %q, got %q", wantUA, gotUA)
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if info.UserAgent != wantUA {
		t.Errorf("info.UserAgent: want %q, got %q", wantUA, info.UserAgent)
	}
}

func TestFetch_CustomUserAgent(t *testing.T) {
	uaCh := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uaCh <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>X</title></head><body></body></html>"))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	customUA := "MyCustomAgent/1.0"
	info, err := Fetch(ctx, srv.URL, customUA)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	var gotUA string
	select {
	case gotUA = <-uaCh:
	default:
		t.Fatalf("server did not receive request")
	}
	if gotUA != customUA {
		t.Errorf("User-Agent: want %q, got %q", customUA, gotUA)
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if info.UserAgent != customUA {
		t.Errorf("info.UserAgent: want %q, got %q", customUA, info.UserAgent)
	}
}

func TestFetch_BodyCloseReturnsError(t *testing.T) {
	orig := http.DefaultTransport
	defer func() { http.DefaultTransport = orig }()

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		b := &failingBody{
			firstData: []byte("<html><head><title>X</title></head><body></body></html>"),
			firstErr:  io.ErrUnexpectedEOF,
			closeErr:  errors.New("simulated close error"),
		}
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       b,
			Request:    req,
		}, nil
	})
	http.DefaultTransport = rt

	ctx := context.Background()
	_, err := Fetch(ctx, "http://example.invalid/", "")
	if err == nil {
		t.Fatalf("expected error when response body Close returns error, got nil")
	}
}

func TestFetch_BadURL(t *testing.T) {
	ctx := context.Background()
	_, err := Fetch(ctx, "://bad-url", "")
	if err == nil {
		t.Fatalf("expected error for bad URL, got nil")
	}
}

func TestFetch_NonexistentHost(t *testing.T) {
	ctx := context.Background()
	_, err := Fetch(ctx, "https://example.invalid/", "")
	if err == nil {
		t.Fatalf("expected error for nonexistent host, got nil")
	}
}
func TestFetch_Redirect(t *testing.T) {
	// final server that will receive the redirected request
	finalPathCh := make(chan string, 1)
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalPathCh <- r.URL.String() // typically a path like "/final"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>Final Title</title></head><body></body></html>"))
	})
	finalSrv := httptest.NewServer(finalHandler)
	defer finalSrv.Close()

	// redirecting server that redirects to finalSrv
	redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL+"/final", http.StatusFound)
	})
	redirectSrv := httptest.NewServer(redirectHandler)
	defer redirectSrv.Close()

	ctx := context.Background()
	startURL := redirectSrv.URL + "/start"
	info, err := Fetch(ctx, startURL, "")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if info.URL != startURL {
		t.Errorf("URL: want %q, got %q", startURL, info.URL)
	}

	// build expected final location from captured path
	var gotFinalPath string
	select {
	case gotFinalPath = <-finalPathCh:
	default:
		t.Fatalf("final server did not receive request")
	}
	expectedLocation := finalSrv.URL + gotFinalPath
	if info.Location != expectedLocation {
		t.Errorf("Location: want %q, got %q", expectedLocation, info.Location)
	}
	if info.Title != "Final Title" {
		t.Errorf("Title: want %q, got %q", "Final Title", info.Title)
	}
}

func TestFetch_ShiftJIS_Encoding(t *testing.T) {
	title := "シフトJISのタイトル"
	desc := "説明SJ"
	html := fmt.Sprintf("<!doctype html><html><head><title>%s</title><meta name=\"description\" content=\"%s\"></head><body></body></html>", title, desc)

	encHTML, _, err := transform.String(japanese.ShiftJIS.NewEncoder(), html)
	if err != nil {
		t.Fatalf("failed to encode html to Shift_JIS: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=Shift_JIS")
		_, _ = w.Write([]byte(encHTML))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	info, err := Fetch(ctx, srv.URL, "")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if info.Title != title {
		t.Errorf("Title: want %q, got %q", title, info.Title)
	}
	if info.Description != desc {
		t.Errorf("Description: want %q, got %q", desc, info.Description)
	}
}

func TestFetch_ISO2022JP_Encoding(t *testing.T) {
	title := "ISO2022JPのタイトル"
	desc := "説明ISO"
	html := fmt.Sprintf("<!doctype html><html><head><title>%s</title><meta name=\"description\" content=\"%s\"></head><body></body></html>", title, desc)

	encHTML, _, err := transform.String(japanese.ISO2022JP.NewEncoder(), html)
	if err != nil {
		t.Fatalf("failed to encode html to ISO-2022-JP: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=ISO-2022-JP")
		_, _ = w.Write([]byte(encHTML))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	info, err := Fetch(ctx, srv.URL, "")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if info.Title != title {
		t.Errorf("Title: want %q, got %q", title, info.Title)
	}
	if info.Description != desc {
		t.Errorf("Description: want %q, got %q", desc, info.Description)
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
