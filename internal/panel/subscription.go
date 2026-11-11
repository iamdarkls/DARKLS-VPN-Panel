package panel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const subscriptionLimit = 512 << 10

type subscriptionConfig struct {
	URL     string `json:"url"`
	Profile string `json:"profile"`
	Bound   string `json:"bound"`
	Auto    bool   `json:"auto"`
}

type subscriptionResult struct {
	At      int64  `json:"checked_at"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

var subscriptionSpecial = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

var subscriptionPublicV6 = netip.MustParsePrefix("2000::/3")

func subscriptionPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Zone() != "" ||
		(ip.Is6() && !subscriptionPublicV6.Contains(ip)) {
		return false
	}
	for _, prefix := range subscriptionSpecial {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func subscriptionURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if len(raw) > 4096 || err != nil || u.Scheme != "https" || u.Hostname() == "" ||
		u.User != nil || strings.Contains(raw, "#") {
		return nil, errors.New("нужна HTTPS-ссылка на подписку без логина и фрагмента")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("некорректный порт сервера подписки")
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, e := netip.ParseAddr(host); e == nil {
		if subscriptionPublic(ip) {
			return u, nil
		}
	} else if strings.Contains(host, ".") && !strings.HasSuffix(host, ".local") &&
		!strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".lan") {
		return u, nil
	}
	return nil, errors.New("нужен публичный адрес сервера подписки")
}

func subscriptionAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("адрес не разрешается")
	}
	for _, ip := range ips {
		if !subscriptionPublic(ip) {
			return nil, errors.New("адрес не является публичным")
		}
	}
	return ips, nil
}

// No redirects or environment proxy. Dial the checked address without a second
// DNS lookup; TLS still verifies the hostname from the original subscription URL.
func fetchSubscription(ctx context.Context, raw string) ([]byte, error) {
	u, err := subscriptionURL(raw)
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = sharedRoots().Clone()
	} else {
		roots.AppendCertsFromPEM(probeRoots)
	}
	dialer := net.Dialer{Timeout: 4 * time.Second}
	tr := &http.Transport{
		TLSClientConfig:        &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, endpoint string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(endpoint)
			if e != nil {
				return nil, e
			}
			ips, e := subscriptionAddresses(ctx, host)
			if e != nil {
				return nil, e
			}
			for _, ip := range ips {
				if conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
					return conn, nil
				}
			}
			return nil, errors.New("нет соединения с сервером подписки")
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("не удалось подготовить запрос подписки")
	}
	req.Header.Set("User-Agent", "DARKLS-VPN-Panel")
	req.Header.Set("Accept", "text/plain, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("не удалось получить подписку по HTTPS; проверьте адрес и сертификат")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("сервер подписки вернул HTTP " + strconv.Itoa(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, subscriptionLimit+1))
	if err != nil || len(data) > subscriptionLimit {
		return nil, errors.New("ответ подписки не прочитан или больше 512 КиБ")
	}
	return data, nil
}

func selectSubscription(data []byte, name string) (map[string]any, error) {
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
	var selected string
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "vless://") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || (name != "" && u.Fragment != name) {
			continue
		}
		if selected != "" {
			return nil, errors.New("найдено несколько VLESS-профилей; укажите уникальное точное имя")
		}
		selected = line
	}
	if selected == "" {
		return nil, errors.New("в подписке нет подходящего VLESS-профиля")
	}
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

func (s *Server) subscriptionPath() string {
	return filepath.Join(s.dir, "subscription.json")
}

func (s *Server) subscriptionSettings() subscriptionConfig {
	var c subscriptionConfig
	if readJSON(s.subscriptionPath(), &c) != nil {
		return subscriptionConfig{}
	}
	return c
}

func (s *Server) subscriptionResult() subscriptionResult {
	var result subscriptionResult
	_ = readJSON(s.runtimePath("subscription-status.json"), &result)
	return result
}

func (s *Server) subscriptionCurrent() (string, error) {
	if s.profileStatus()["active"] != "primary" {
		return "", errors.New("сначала подключите основной профиль")
	}
	out, err := s.connectionOutbound("current")
	if err != nil {
		return "", errors.New("не удалось прочитать основное подключение")
	}
	return outboundHash(out), nil
}

func (s *Server) subscriptionStatus() map[string]any {
	c := s.subscriptionSettings()
	current, _ := s.subscriptionCurrent()
	return map[string]any{"configured": c.URL != "", "auto": c.Auto, "profile": c.Profile,
		"bound": c.Bound != "" && c.Bound == current, "result": s.subscriptionResult()}
}

func (s *Server) subscriptionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Password string `json:"password"`
		Action   string `json:"action"`
		URL      string `json:"url"`
		Profile  string `json:"profile"`
		Auto     bool   `json:"auto"`
	}
	if err := decode(w, r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.currentPassword(r, req.Password); err != nil {
		fail(w, 403, err)
		return
	}
	if !s.subscriptionMu.TryLock() {
		fail(w, 409, errors.New("проверка подписки уже выполняется"))
		return
	}
	defer s.subscriptionMu.Unlock()

	switch req.Action {
	case "refresh":
		code, err := s.refreshSubscription(r.Context(), s.subscriptionSettings())
		if err != nil {
			fail(w, 400, err)
		} else {
			respond(w, 200, map[string]string{"code": code})
		}
		return
	case "remove":
		if err := os.Remove(s.subscriptionPath()); err != nil && !os.IsNotExist(err) {
			fail(w, 500, errors.New("не удалось удалить подписку"))
			return
		}
	case "save":
		if !s.mutation.TryLock() {
			fail(w, 409, errors.New("другая операция выполняется"))
			return
		}
		defer s.mutation.Unlock()
		current, err := s.subscriptionCurrent()
		if err != nil {
			fail(w, 409, err)
			return
		}
		raw := strings.TrimSpace(req.URL)
		if raw == "" {
			raw = s.subscriptionSettings().URL
		}
		if _, err = subscriptionURL(raw); err != nil {
			fail(w, 400, err)
			return
		}
		name := strings.TrimSpace(req.Profile)
		if name != "" && validConnectionName(name) != nil {
			fail(w, 400, errors.New("укажите имя профиля длиной до 60 символов"))
			return
		}
		if err = atomicWrite(s.subscriptionPath(), jsonBytes(subscriptionConfig{raw, name, current, req.Auto})); err != nil {
			fail(w, 500, errors.New("не удалось сохранить подписку"))
			return
		}
	default:
		fail(w, 400, errors.New("неизвестное действие подписки"))
		return
	}
	_ = os.Remove(s.runtimePath("subscription-status.json"))
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) refreshSubscription(ctx context.Context, c subscriptionConfig) (code string, err error) {
	if c.URL == "" {
		return "", errors.New("сначала сохраните ссылку на подписку")
	}
	if current, _ := s.subscriptionCurrent(); current != c.Bound || current == "" || s.disabled() {
		return "", errors.New("включите основной VPN и привяжите подписку к текущему подключению")
	}
	defer func() {
		if code != "" && ctx.Err() == nil {
			result := subscriptionResult{At: time.Now().Unix(), Code: code}
			if err != nil {
				result.Message = err.Error()
			}
			_ = atomicWrite(s.runtimePath("subscription-status.json"), jsonBytes(result))
			switch code {
			case "updated":
				appendEvent("connection", "Основное подключение обновлено из подписки")
			case "error":
				appendEvent("connection_error", "Не удалось обновить профиль из подписки; подробности в разделе VPN-серверы")
			}
		}
	}()
	data, err := fetchSubscription(ctx, c.URL)
	if err != nil {
		return "error", err
	}
	out, err := selectSubscription(data, c.Profile)
	if err != nil {
		return "error", err
	}
	hash := outboundHash(out)
	if hash != c.Bound {
		host, _ := outboundEndpoint(out)
		dnsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = subscriptionAddresses(dnsCtx, host)
		cancel()
		if err != nil {
			return "error", errors.New("новый VPN-сервер не разрешается в публичный адрес")
		}
	}
	if !s.mutation.TryLock() {
		return "", errors.New("другая операция выполняется; повторите позже")
	}
	defer s.mutation.Unlock()
	if current, _ := s.subscriptionCurrent(); current != c.Bound || s.disabled() ||
		updateBusy() || coreJobActive(s.coreJob()) || s.pendingRecovery() {
		return "", errors.New("подключение или состояние панели изменилось; повторите проверку позже")
	}
	if hash == c.Bound {
		return "unchanged", nil
	}
	if !s.diagnosticMu.TryLock() {
		return "", errors.New("диагностика занята; повторите позже")
	}
	defer s.diagnosticMu.Unlock()
	release, err := s.pause()
	if err != nil {
		return "", err
	}
	defer release()
	probeCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	if err = s.activateConnection(probeCtx, out, "subscription"); err != nil {
		return "error", errors.New("новый профиль не применён; проверьте состояние подключения и восстановления")
	}
	c.Bound = hash
	if err = atomicWrite(s.subscriptionPath(), jsonBytes(c)); err != nil {
		return "error", errors.New("профиль применён, но подписку нужно привязать заново")
	}
	return "updated", nil
}

func subscriptionDue(result subscriptionResult, now time.Time) bool {
	delay := 6 * time.Hour
	if result.Code == "error" {
		delay = time.Hour
	}
	age := now.Sub(time.Unix(result.At, 0))
	return result.At <= 0 || age < 0 || age >= delay
}

func (s *Server) subscriptionTick(ctx context.Context) {
	if time.Now().Year() < 2025 || updateBusy() || s.pendingRecovery() || !s.subscriptionMu.TryLock() {
		return
	}
	defer s.subscriptionMu.Unlock()
	c := s.subscriptionSettings()
	if c.URL != "" && c.Auto && subscriptionDue(s.subscriptionResult(), time.Now()) {
		_, _ = s.refreshSubscription(ctx, c)
	}
}
