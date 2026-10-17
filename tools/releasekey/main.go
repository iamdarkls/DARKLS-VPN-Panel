// releasekey runs on the maintainer's Windows PC and in GitHub Actions.
// The private key is never included in the repository or release package.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	mode := flag.String("mode", "keygen", "keygen or sign")
	private := flag.String("private", "", "private key path (keygen only)")
	public := flag.String("public", "github-public-key.txt", "public key file")
	archive := flag.String("archive", "", "release ZIP")
	version := flag.String("version", "", "release version")
	out := flag.String("out", "dist", "output directory")
	flag.Parse()
	if e := run(*mode, *private, *public, *archive, *version, *out); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(mode, private, public, archive, version, out string) error {
	var key ed25519.PrivateKey
	if mode == "keygen" {
		if private == "" {
			return fmt.Errorf("private path required")
		}
		b, e := os.ReadFile(private)
		if os.IsNotExist(e) {
			_, key, e = ed25519.GenerateKey(rand.Reader)
			if e != nil {
				return e
			}
			if e = os.MkdirAll(filepath.Dir(private), 0700); e != nil {
				return e
			}
			f, e := os.OpenFile(private, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, e = f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
			f.Close()
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		} else {
			raw, e := base64.StdEncoding.DecodeString(stringTrim(b))
			if e != nil || len(raw) != ed25519.PrivateKeySize {
				return fmt.Errorf("invalid existing key")
			}
			key = raw
		}
		if e := os.WriteFile(public, []byte(base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))+"\n"), 0644); e != nil {
			return e
		}
		fmt.Println("Signing key ready; public key written")
		return nil
	}
	if mode != "sign" {
		return fmt.Errorf("unknown mode")
	}
	raw, e := base64.StdEncoding.DecodeString(os.Getenv("RELEASE_SIGNING_KEY"))
	if e != nil || len(raw) != ed25519.PrivateKeySize {
		return fmt.Errorf("RELEASE_SIGNING_KEY is missing or invalid")
	}
	key = raw
	pub, e := os.ReadFile(public)
	if e != nil {
		return e
	}
	if stringTrim(pub) != base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) {
		return fmt.Errorf("signing key does not match pinned public key")
	}
	b, e := os.ReadFile(archive)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(b)
	m := struct {
		Format     int    `json:"format"`
		Kind       string `json:"kind"`
		Repository string `json:"repository"`
		Version    string `json:"version"`
		Arch       string `json:"arch"`
		Asset      string `json:"asset"`
		SHA256     string `json:"sha256"`
		Size       int    `json:"size"`
	}{1, "panel", "iamdarkls/DARKLS-VPN-Panel", version, "arm64", filepath.Base(archive), hex.EncodeToString(sum[:]), len(b)}
	data, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return e
	}
	data = append(data, '\n')
	sig := ed25519.Sign(key, data)
	if e = os.MkdirAll(out, 0755); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(out, "panel-release.json"), data, 0644); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(out, "panel-release.sig"), []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0644)
}
func stringTrim(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return string(b)
}
