package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Durable writes use same-directory rename and fsync. Runtime JSON uses this
// path too, so readers never observe a partially written status document.
func atomicWrite(path string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".panel-write-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func jsonBytes(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }

func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}

func limited(b []byte) string {
	if len(b) > 3000 {
		b = b[len(b)-3000:]
	}
	return string(b)
}
