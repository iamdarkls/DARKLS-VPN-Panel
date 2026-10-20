package panel

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkNormalizeDomain(b *testing.B) {
	domains := []string{"rr1---sn.example.googlevideo.com", "chatgpt.com", "https://CDN.Example.COM/image?id=1", "xn--e1afmkfd.xn--p1ai"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := normalizeDomain(domains[i%len(domains)]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStateRead(b *testing.B) {
	root := b.TempDir()
	s := newServer(root, "127.0.0.1:8088", nil)
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		b.Fatal(err)
	}
	state, err := importState([]byte("youtube.com\nchatgpt.com\napi.ipify.org\n"))
	if err != nil {
		b.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(state)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.state(); err != nil {
			b.Fatal(err)
		}
	}
}
