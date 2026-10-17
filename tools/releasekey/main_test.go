package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyGenerationIsStableAndReleaseSignatureVerifies(t *testing.T) {
	d := t.TempDir()
	keyPath := filepath.Join(d, "private", "signing.key")
	pubPath := filepath.Join(d, "public.txt")
	if e := run("keygen", keyPath, pubPath, "", "", ""); e != nil {
		t.Fatal(e)
	}
	first, _ := os.ReadFile(keyPath)
	if e := run("keygen", keyPath, pubPath, "", "", ""); e != nil {
		t.Fatal(e)
	}
	second, _ := os.ReadFile(keyPath)
	if string(first) != string(second) {
		t.Fatal("key rotated on retry")
	}
	zip := filepath.Join(d, "DARKLS-VPN-Panel-v1.2.1.zip")
	os.WriteFile(zip, []byte("test archive bytes"), 0600)
	t.Setenv("RELEASE_SIGNING_KEY", strings.TrimSpace(string(first)))
	if e := run("sign", "", pubPath, zip, "1.2.1", d); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(d, "panel-release.json"))
	s, _ := os.ReadFile(filepath.Join(d, "panel-release.sig"))
	sig, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(s)))
	p, _ := os.ReadFile(pubPath)
	pub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(p)))
	if !ed25519.Verify(pub, b, sig) {
		t.Fatal("signature did not verify")
	}
	os.WriteFile(pubPath, []byte("wrong public key"), 0644)
	if run("sign", "", pubPath, zip, "1.2.1", d) == nil {
		t.Fatal("signed using a different key than pinned in source")
	}
}
