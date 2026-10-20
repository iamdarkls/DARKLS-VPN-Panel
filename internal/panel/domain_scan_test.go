package panel

import (
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
)

// Keep exactly the previous hostname rules, including malformed labels and IDN.
func TestDomainLabelScanMatchesPreviousRules(t *testing.T) {
	previous := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	legacy := func(domain string) bool {
		for _, label := range strings.Split(domain, ".") {
			if !previous.MatchString(label) {
				return false
			}
		}
		return true
	}
	cases := []string{"", "a", "a.b", "-a.b", "a-.b", "a..b", "a.", ".a", "xn--e1afmkfd.xn--p1ai", "пример.рф", "A.com", strings.Repeat("a", 63) + ".com", strings.Repeat("a", 64) + ".com", "a_b.com", "a\x00b.com"}
	rng := rand.New(rand.NewPCG(3, 7))
	alphabet := []byte("abcz019-._ABC\x00")
	for range 10000 {
		value := make([]byte, rng.IntN(100))
		for i := range value {
			value[i] = alphabet[rng.IntN(len(alphabet))]
		}
		cases = append(cases, string(value))
	}
	for _, domain := range cases {
		if got, want := validDomainLabels(domain), legacy(domain); got != want {
			t.Fatalf("%q: %v != %v", domain, got, want)
		}
	}
}
