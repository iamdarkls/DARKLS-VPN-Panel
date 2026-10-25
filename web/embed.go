// Package web serves build-time compressed assets without retaining decoded copies.
package web

//go:generate npm run build

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type compressedAsset struct {
	data        string
	contentType string
	etag        string
	size        int
}

// encodingPreference honours explicit gzip/identity exclusions and wildcards.
// Unknown or malformed encodings are never selected.
func encodingPreference(header string) (gzipOK, identityOK bool) {
	qualities := map[string]float64{}
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "gzip" && token != "identity" && token != "*" {
			continue
		}
		q := 1.0
		for _, param := range strings.Split(params, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if !ok || err != nil || parsed < 0 || parsed > 1 {
					q = 0
				} else {
					q = parsed
				}
			}
		}
		qualities[token] = q
	}
	gzipQ, explicit := qualities["gzip"]
	if !explicit {
		gzipQ = qualities["*"]
	}
	identityQ, explicit := qualities["identity"]
	if !explicit {
		identityQ = 1
		if wildcard, ok := qualities["*"]; ok && wildcard == 0 {
			identityQ = 0
		}
	}
	return gzipQ > 0, identityQ > 0
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// ServeAsset sends gzip directly to capable browsers. Identity responses stream
// through a temporary decoder; no map of permanently expanded files is built.
func ServeAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := compressedAssets[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	gzipOK, identityOK := encodingPreference(r.Header.Get("Accept-Encoding"))
	w.Header().Set("Vary", "Accept-Encoding")
	if !gzipOK && !identityOK {
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	if r.URL.Path == "/" {
		w.Header().Set("Cache-Control", "no-store")
	}
	if r.URL.Path != "/" {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", asset.etag)
		if etagMatches(r.Header.Get("If-None-Match"), asset.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	length := asset.size
	if gzipOK {
		length = len(asset.data)
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.Header().Set("Content-Length", strconv.Itoa(length))
	if r.Method == http.MethodHead {
		return
	}
	if gzipOK {
		_, _ = io.Copy(w, strings.NewReader(asset.data))
		return
	}
	reader, err := gzip.NewReader(strings.NewReader(asset.data))
	if err != nil {
		http.Error(w, "embedded asset error", http.StatusInternalServerError)
		return
	}
	defer reader.Close()
	_, _ = io.Copy(w, reader)
}

// HasAsset is the exact build-generated allowlist; source files and arbitrary
// paths beneath /assets are never made public.
func HasAsset(path string) bool { _, ok := compressedAssets[path]; return ok }
