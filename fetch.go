package webinfo

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/goark/errs"
	"github.com/goark/fetch"
	"github.com/mattn/go-encoding"
	"golang.org/x/net/html/charset"
)

// Fetch retrieves metadata from the web page at urlStr and returns it as a *Webinfo.
//
// Behavior:
//   - Parses urlStr and performs an HTTP GET using the provided context (ctx).
//   - If userAgent is empty, a default dummy User-Agent string is used.
//   - Uses an HTTP client and sets the User-Agent request header.
//   - Reads up to the first 1024 bytes of the response to detect the page character
//     encoding via charset.DetermineEncoding (also considers the response Content-Type).
//     If an encoding is detected or inferred by name, the response body is decoded
//     accordingly before HTML parsing.
//
// Parsing and extracted fields:
// - Parses the document head with goquery and extracts:
//   - Title: from <title>, then overridden by meta[property="twitter:title"] or meta[property="og:title"] if present.
//   - Description: from meta[name="description"], then overridden by meta[property="twitter:description"] or meta[property="og:description"].
//   - ImageURL: from meta[property="twitter:image"] or meta[property="og:image"].
//   - Canonical: from link[rel="canonical"].
//
// - The returned Webinfo contains at least:
//   - URL: the original urlStr (string form).
//   - Location: the final request URL (after redirects) from the response.
//   - UserAgent: the User-Agent actually used.
//
// Error handling and resource cleanup:
// - Network, URL parsing, encoding detection, and HTML parsing errors are wrapped with contextual information (including the URL).
// - The response body is closed in a deferred function; any close error is joined with the returned error.
// - On error, Fetch returns a nil *Webinfo and a non-nil error.
//
// Notes and guarantees:
// - The first 1024 bytes are peeked (without advancing the reader) to determine encoding.
// - DetermineEncoding's boolean return value is ignored (some encodings like Shift_JIS may be reported inconsistently); the detected encoding or a named encoding (via encoding.GetEncoding) is preferred.
// - The function honors context cancellation for the HTTP request.
// - Caller should assume that a non-nil *Webinfo is returned only on success; otherwise, info is nil.
func Fetch(ctx context.Context, urlStr, userAgent string) (info *Webinfo, err error) {
	// check arguments
	parsed, uerr := fetch.URL(strings.TrimSpace(urlStr))
	if uerr != nil {
		info = nil
		err = errs.Wrap(uerr, errs.WithContext("url", urlStr))
		return
	}
	userAgent = getUserAgent(userAgent)

	// fetch web page
	resp, ferr := fetch.New(fetch.WithHTTPClient(&http.Client{})).GetWithContext(
		ctx,
		parsed,
		fetch.WithRequestHeaderSet("User-Agent", userAgent),
	)
	if ferr != nil {
		info = nil
		err = errs.Wrap(ferr, errs.WithContext("url", parsed.String()))
		return
	}
	defer func() {
		if cerr := resp.Close(); cerr != nil && cerr != os.ErrClosed {
			err = errs.Join(errs.Wrap(cerr, errs.WithContext("url", parsed.String())), err)
		}
	}()

	br := bufio.NewReader(resp.Body()) // buffered reader
	var r io.Reader = br
	// detect character encoding
	if data, perr := br.Peek(1024); perr != nil && perr != io.EOF { //next 1024 bytes without advancing the reader.
		info = nil
		err = errs.Wrap(perr, errs.WithContext("url", urlStr))
		return
	} else {
		// DetermineEncoding determines the encoding of an HTML document by examining up to the first 1024 bytes of content and the declared Content-Type.
		enc, name, _ := charset.DetermineEncoding(data, resp.Header().Get("content-type")) // Ignore the 'ok' return value because DetermineEncoding may not return it correctly for Shift_JIS
		if enc != nil {
			r = enc.NewDecoder().Reader(br) // use detected encoding
		} else if len(name) > 0 {
			if enc := encoding.GetEncoding(name); enc != nil {
				r = enc.NewDecoder().Reader(br) // use specified encoding
			}
		}
	}

	// parse HTML metadata
	info = &Webinfo{
		URL:       parsed.String(),
		Location:  resp.Request().URL.String(),
		UserAgent: userAgent,
	}
	var doc *goquery.Document
	doc, err = goquery.NewDocumentFromReader(r)
	if err != nil {
		info = nil
		err = errs.Wrap(err, errs.WithContext("url", parsed.String()))
		return
	}
	doc.Find("head").Each(func(_ int, s *goquery.Selection) {
		s.Find("title").Each(func(_ int, s *goquery.Selection) {
			t := s.Text()
			if len(t) > 0 {
				info.Title = strings.TrimSpace(t)
			}
		})
		s.Find(`meta[property="twitter:title"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.Title = strings.TrimSpace(v)
			}
		})
		s.Find(`meta[property="og:title"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.Title = strings.TrimSpace(v)
			}
		})
		s.Find(`meta[name="description"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.Description = strings.TrimSpace(v)
			}
		})
		s.Find(`meta[property="twitter:description"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.Description = strings.TrimSpace(v)
			}
		})
		s.Find(`meta[property="og:description"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.Description = strings.TrimSpace(v)
			}
		})
		s.Find(`meta[property="twitter:image"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.ImageURL = strings.TrimSpace(v)
			}
		})
		s.Find(`meta[property="og:image"]`).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("content"); ok && len(v) > 0 {
				info.ImageURL = strings.TrimSpace(v)
			}
		})
		s.Find("link[rel='canonical']").Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr("href"); ok && len(v) > 0 {
				info.Canonical = strings.TrimSpace(v)
			}
		})
	})
	return
}

// getUserAgent returns a user-agent string to use for HTTP requests.
// It trims whitespace from the provided ua parameter; if the trimmed value is empty,
// it returns a default (dummy) User-Agent string ("Mozilla/5.0 (Windows NT 6.1; rv:11.0) Gecko/20100101 Firefox/11.0").
// Otherwise, it returns the supplied ua unchanged.
func getUserAgent(ua string) string {
	if len(strings.TrimSpace(ua)) == 0 {
		return "Mozilla/5.0 (Windows NT 6.1; rv:11.0) Gecko/20100101 Firefox/11.0" //dummy user-agent string
	}
	return ua
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
