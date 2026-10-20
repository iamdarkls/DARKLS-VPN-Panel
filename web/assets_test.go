package web

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCompressedAssetsMatchEditableSource(t *testing.T) {
	for url, asset := range compressedAssets {
		name := strings.TrimPrefix(url, "/")
		if name == "" {
			name = "index.html"
		}
		want, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(strings.NewReader(asset.data))
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(got) != string(want) || asset.size != len(want) {
			t.Errorf("stale compressed source: %s", name)
		}
	}
}

func TestAssetEncodingHeadAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		header, encoding string
		status           int
	}{
		{"", "", 200}, {"gzip", "gzip", 200}, {"GZIP; q=0.5", "gzip", 200},
		{"gzip;q=0", "", 200}, {"br", "", 200}, {"*;q=1", "gzip", 200},
		{"gzip;q=0, *;q=1", "", 200}, {"gzip;q=0,identity;q=0", "", 406},
		{"*;q=0", "", 406}, {"gzip;q=broken", "", 200}, {"gzip;q=2", "", 200},
	} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+"/"+tc.header, func(t *testing.T) {
				req := httptest.NewRequest(method, "/js/core/ui.js", nil)
				req.Header.Set("Accept-Encoding", tc.header)
				result := httptest.NewRecorder()
				ServeAsset(result, req)
				if result.Code != tc.status || result.Header().Get("Content-Encoding") != tc.encoding {
					t.Fatalf("%d %v", result.Code, result.Header())
				}
				if result.Header().Get("Vary") != "Accept-Encoding" {
					t.Fatal("encoding missing from cache key")
				}
				if method == "HEAD" && result.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
				if tc.status != 200 {
					return
				}
				asset := compressedAssets["/js/core/ui.js"]
				size := asset.size
				if tc.encoding == "gzip" {
					size = len(asset.data)
				}
				if result.Header().Get("Content-Length") != strconv.Itoa(size) {
					t.Fatal("wrong encoded length")
				}
				if method == "GET" {
					body := result.Body.Bytes()
					if tc.encoding == "gzip" {
						reader, err := gzip.NewReader(strings.NewReader(string(body)))
						if err != nil {
							t.Fatal(err)
						}
						body, err = io.ReadAll(reader)
						reader.Close()
						if err != nil {
							t.Fatal(err)
						}
					}
					source, err := os.ReadFile("js/core/ui.js")
					if err != nil {
						t.Fatal(err)
					}
					if string(source) != string(body) {
						t.Fatal("browser receives different source")
					}
				}
			})
		}
	}
}

func TestStaticRevalidationDoesNotCacheHTML(t *testing.T) {
	asset := compressedAssets["/js/core/ui.js"]
	for _, header := range []string{asset.etag, "\"unrelated\", " + asset.etag, "*"} {
		req := httptest.NewRequest("GET", "/js/core/ui.js", nil)
		req.Header.Set("If-None-Match", header)
		result := httptest.NewRecorder()
		ServeAsset(result, req)
		if result.Code != 304 || result.Body.Len() != 0 || result.Header().Get("Content-Length") != "" || result.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("bad revalidation: %d %v", result.Code, result.Header())
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("If-None-Match", "*")
	result := httptest.NewRecorder()
	ServeAsset(result, req)
	if result.Code != 200 || result.Header().Get("ETag") != "" || result.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("HTML must remain fresh")
	}
}
