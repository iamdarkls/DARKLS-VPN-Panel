package panel

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

// Parsing is shared by catalog discovery and profile selection. It never fetches
// remote resources or changes the running VPN.
type subscriptionEntry struct {
	name string
	link string
}

func subscriptionEntries(data []byte) []subscriptionEntry {
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "\ufeff"))
	if !strings.Contains(body, "://") {
		encoded := strings.Join(strings.Fields(body), "")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if decoded, err := enc.DecodeString(encoded); err == nil {
				body = string(decoded)
				break
			}
		}
	}
	var entries []subscriptionEntry
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "vless://") {
			continue
		}
		if u, err := url.Parse(line); err == nil {
			entries = append(entries, subscriptionEntry{u.Fragment, line})
		}
	}
	return entries
}

func selectSubscription(data []byte, name string) (map[string]any, error) {
	var selected string
	for _, entry := range subscriptionEntries(data) {
		if name != "" && entry.name != name {
			continue
		}
		if selected != "" {
			return nil, errors.New("найдено несколько VLESS-профилей; загрузите список и выберите уникальный профиль")
		}
		selected = entry.link
	}
	if selected == "" {
		return nil, errors.New("в подписке нет подходящего VLESS-профиля")
	}
	return parseSubscriptionOutbound(selected)
}

type subscriptionProfile struct {
	Name string `json:"name"`
}

type subscriptionCatalog struct {
	Profiles    []subscriptionProfile `json:"profiles"`
	Unavailable int                   `json:"unavailable"`
	Truncated   bool                  `json:"truncated"`
}

func listSubscriptionProfiles(data []byte) subscriptionCatalog {
	entries := subscriptionEntries(data)
	catalog := subscriptionCatalog{Profiles: []subscriptionProfile{}}
	counts := make(map[string]int, len(entries))
	for _, entry := range entries {
		counts[entry.name]++
	}
	for _, entry := range entries {
		name := entry.name
		// An empty selector means "the only VLESS entry", never "the unnamed entry".
		if (name == "" && len(entries) != 1) || counts[name] != 1 ||
			(name != "" && (strings.TrimSpace(name) != name || validConnectionName(name) != nil || redact(name) != name)) {
			catalog.Unavailable++
			continue
		}
		if len(catalog.Profiles) == subscriptionProfileLimit {
			catalog.Truncated = true
			break
		}
		if _, err := parseSubscriptionOutbound(entry.link); err != nil {
			catalog.Unavailable++
			continue
		}
		catalog.Profiles = append(catalog.Profiles, subscriptionProfile{Name: name})
	}
	return catalog
}

func parseSubscriptionOutbound(selected string) (map[string]any, error) {
	out, err := parseBackup(selected)
	if err != nil {
		return nil, errors.New("найденный VLESS-профиль не поддерживается")
	}
	stream, _ := out["streamSettings"].(map[string]any)
	tlsSettings, _ := stream["tlsSettings"].(map[string]any)
	xhttp, _ := stream["xhttpSettings"].(map[string]any)
	extra, _ := xhttp["extra"].(map[string]any)
	if (stream["security"] != "reality" && stream["security"] != "tls") ||
		tlsSettings["allowInsecure"] == true {
		return nil, errors.New("для подписки нужен VLESS с REALITY или проверяемым TLS")
	}
	for key := range extra {
		if strings.EqualFold(key, "downloadSettings") {
			return nil, errors.New("подписка не поддерживает отдельный XHTTP downloadSettings")
		}
	}
	return out, nil
}
