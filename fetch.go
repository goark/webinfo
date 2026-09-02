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

// Fetch retrieves metadata from a web page and returns it as Webinfo.
//
// It fetches the page with the given context and User-Agent (or a default one when
// empty), peeks up to 1024 bytes to determine encoding, then parses the head
// section with goquery.
//
// Extraction precedence is kept explicit:
// title: title -> twitter:title -> og:title
// description: meta[name=description] -> twitter:description -> og:description
// image: twitter:image -> og:image
//
// Returned errors are wrapped with context. Response close errors are joined.
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

// DefaultUserAgent returns the default User-Agent string.
func DefaultUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0" //dummy user-agent string (ref: https://note.com/kzstock/n/nae18de160dca)
}

// getUserAgent returns ua if non-empty after trimming; otherwise it returns
// the package default User-Agent string.
func getUserAgent(ua string) string {
	if len(strings.TrimSpace(ua)) == 0 {
		return DefaultUserAgent()
	}
	return ua
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
