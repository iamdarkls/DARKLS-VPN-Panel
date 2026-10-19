package panel

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// State validation and DNS configuration are shared by HTTP, CLI and recovery.
// Read fresh state from disk: another CLI process can commit under apply.lock.
type Group struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Domains       []string `json:"domains"`
	Category      string   `json:"category,omitempty"`
	BlockQUIC     *bool    `json:"block_quic,omitempty"`
	EnabledUntil  int64    `json:"enabled_until,omitempty"`
	TimedPrevious bool     `json:"timed_previous,omitempty"`
	QUICUntil     int64    `json:"quic_until,omitempty"`
	QUICPrevious  bool     `json:"quic_previous,omitempty"`
}

type State struct {
	Version int          `json:"version"`
	Groups  []Group      `json:"groups"`
	Policy  DevicePolicy `json:"policy"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeDomain(raw string) (string, error) {
	d := strings.TrimSpace(raw)
	if strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") {
		u, e := url.Parse(d)
		if e != nil || u.User != nil || u.Hostname() == "" {
			return "", errors.New("неверный адрес сайта")
		}
		d = u.Hostname()
	}
	d = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(d, "*."), "."))
	if len(d) > 253 || len(d) < 3 || !strings.Contains(d, ".") {
		return "", fmt.Errorf("неверный домен: %q", raw)
	}
	if _, e := netip.ParseAddr(d); e == nil {
		return "", errors.New("введите домен сайта, а не IP-адрес")
	}
	for _, p := range strings.Split(d, ".") {
		if !labelPattern.MatchString(p) {
			return "", fmt.Errorf("неверный домен %q; для кириллицы используйте punycode", raw)
		}
	}
	return d, nil
}

func validate(st State) (State, error) {
	if len(st.Groups) > 100 {
		return st, errors.New("не более 100 групп")
	}
	st.Version = 1
	if err := validatePolicy(&st.Policy); err != nil {
		return st, err
	}
	ids := map[string]bool{}
	total := 0
	for i := range st.Groups {
		g := &st.Groups[i]
		g.Name = strings.TrimSpace(g.Name)
		if g.BlockQUIC == nil {
			g.BlockQUIC = boolRef(g.ID == "youtube")
		}
		if g.EnabledUntil < 0 || g.QUICUntil < 0 || (time.Now().Year() >= 2025 && (g.EnabledUntil > time.Now().Add(8*24*time.Hour).Unix() || g.QUICUntil > time.Now().Add(8*24*time.Hour).Unix())) {
			return st, errors.New("неверный срок временной настройки")
		}
		if !idPattern.MatchString(g.ID) || ids[g.ID] {
			return st, errors.New("неверный или повторяющийся идентификатор группы")
		}
		ids[g.ID] = true
		if g.Category == "" {
			g.Category = inferCategory(g.ID)
		}
		if !validCategory(g.Category) {
			return st, errors.New("неверный раздел группы")
		}
		if !utf8.ValidString(g.Name) || utf8.RuneCountInString(g.Name) < 1 || utf8.RuneCountInString(g.Name) > 60 || strings.ContainsAny(g.Name, "\x00\r\n") {
			return st, errors.New("название группы: от 1 до 60 символов")
		}
		seen := map[string]bool{}
		out := []string{}
		for _, raw := range g.Domains {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			d, e := normalizeDomain(raw)
			if e != nil {
				return st, e
			}
			if !seen[d] {
				out = append(out, d)
				seen[d] = true
				total++
			}
		}
		sort.Strings(out)
		g.Domains = out
	}
	if total > 3000 {
		return st, errors.New("не более 3000 доменов")
	}
	return st, nil
}

var presets = []Group{
	{ID: "youtube", Name: "YouTube", Enabled: true, Domains: strings.Fields("youtube.com youtu.be youtube-nocookie.com googlevideo.com ytimg.com youtubei.googleapis.com youtube.googleapis.com yt3.ggpht.com accounts.google.com oauth2.googleapis.com")},
	{ID: "chatgpt", Name: "ChatGPT", Enabled: true, Domains: strings.Fields("chatgpt.com openai.com oaistatic.com oaiusercontent.com")},
	{ID: "instagram", Name: "Instagram", Enabled: true, Domains: strings.Fields("instagram.com cdninstagram.com ig.me fbcdn.net fbsbx.com")},
	{ID: "discord", Name: "Discord", Enabled: true, Domains: strings.Fields("discord.com discord.gg discordapp.com discordapp.net discord.media discordcdn.com dis.gd")},
	{ID: "gemini", Name: "Gemini", Enabled: true, Domains: strings.Fields("gemini.google.com gemini.google aistudio.google.com generativelanguage.googleapis.com accounts.google.com oauth2.googleapis.com gstatic.com googleusercontent.com")},
	{ID: "twitter", Name: "X / Twitter", Enabled: true, Domains: strings.Fields(xDomains)},
	{ID: "telegram", Name: "Telegram", Enabled: true, Domains: strings.Fields("telegram.org telegram.me telegram.dog t.me telesco.pe telegra.ph telegram-cdn.org cdn-telegram.org fragment.com")},
}

func importState(domains []byte) (State, error) {
	existing := map[string]bool{}
	for _, line := range strings.Split(string(domains), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d, e := normalizeDomain(line)
		if e != nil {
			return State{}, e
		}
		existing[d] = true
	}
	used := map[string]bool{}
	st := State{Version: 1, Groups: []Group{}}
	for _, p := range presets {
		g := Group{ID: p.ID, Name: p.Name, Enabled: true, Domains: []string{}}
		for _, d := range p.Domains {
			if existing[d] {
				g.Domains = append(g.Domains, d)
				used[d] = true
			}
		}
		st.Groups = append(st.Groups, g)
	}
	other := Group{ID: "custom", Name: "Мои сайты", Enabled: true, Domains: []string{}}
	for d := range existing {
		if !used[d] {
			other.Domains = append(other.Domains, d)
		}
	}
	st.Groups = append(st.Groups, other)
	st, _ = addCatalogGroups(st)
	return validate(st)
}

func activeDomains(st State) []string {
	m := map[string]bool{}
	for _, g := range st.Groups {
		if g.active(time.Now().Unix()) {
			for _, d := range g.Domains {
				m[d] = true
			}
		}
	}
	out := []string{}
	for d := range m {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func youtubeDomain(d string) bool {
	for _, suffix := range []string{"youtube.com", "googlevideo.com", "youtube-nocookie.com", "ytimg.com"} {
		if d == suffix || strings.HasSuffix(d, "."+suffix) {
			return true
		}
	}
	return false
}

func dnsConfig(domains []string) []byte {
	var b strings.Builder
	b.WriteString("port=10535\nlisten-address=127.0.0.1,192.168.50.1\nbind-interfaces\npid-file=/tmp/dnsmasq-vpn.pid\nno-resolv\nno-hosts\ncache-size=1000\nfilter-AAAA\nserver=127.0.0.1#53\n")
	for _, d := range domains {
		fmt.Fprintf(&b, "server=/%s/127.0.0.1#1053\nipset=/%s/darkls_vpn", d, d)
		if youtubeDomain(d) {
			b.WriteString(",darkls_yt")
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func (s *Server) init() error {
	if e := os.MkdirAll(filepath.Join(s.dir, "backups"), 0700); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(s.dir, "state.json")); os.IsNotExist(e) {
		b, e := os.ReadFile(filepath.Join(s.root, "domains.txt"))
		if e != nil {
			return e
		}
		st, e := importState(b)
		if e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st)); e != nil {
			return e
		}
	}
	tg := filepath.Join(s.dir, "telegram-original.txt")
	if _, e := os.Stat(tg); os.IsNotExist(e) {
		b, e := os.ReadFile(filepath.Join(s.root, "telegram-networks.txt"))
		if e != nil {
			return e
		}
		if e = atomicWrite(tg, b); e != nil {
			return e
		}
	}
	return nil
}

func (s *Server) state() (State, error) {
	var st State
	e := readJSON(filepath.Join(s.dir, "state.json"), &st)
	if e != nil {
		return st, e
	}
	return validate(st)
}
