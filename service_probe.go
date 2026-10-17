package main

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

var serviceDomains = map[string]string{"chatgpt": "chatgpt.com", "youtube": "www.youtube.com", "instagram": "www.instagram.com", "discord": "discord.com", "gemini": "gemini.google.com", "twitter": "x.com", "telegram": "telegram.org"}

type ServiceResult struct {
	Domain     string `json:"domain"`
	State      string `json:"state"`
	Message    string `json:"message"`
	HTTP       int    `json:"http,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func classifyServiceResponse(domain string, resp *http.Response, err error) ServiceResult {
	r := ServiceResult{Domain: domain}
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			r.State = "timeout"
			r.Message = "Тайм-аут HTTPS через VPN"
		} else {
			r.State = "connection_error"
			r.Message = "Ошибка HTTPS через VPN: " + redact(err.Error())
		}
		return r
	}
	r.HTTP = resp.StatusCode
	switch {
	case resp.Header.Get("Cf-Mitigated") == "challenge":
		r.State = "challenge"
		r.Message = "Cloudflare требует проверку в браузере; сервер доступен"
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		r.State = "responded"
		r.Message = "Сайт ответил по HTTPS через VPN"
	default:
		r.State = "http_error"
		r.Message = "Сайт ответил с HTTP-ошибкой; это не доказывает сбой туннеля"
	}
	return r
}
func checkService(ctx context.Context, domain string) ServiceResult {
	started := time.Now()
	finish := func(r ServiceResult) ServiceResult { r.DurationMS = time.Since(started).Milliseconds(); return r }
	dnsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_, e := lookupRouterDNS(dnsCtx, "127.0.0.1:10535", domain)
	cancel()
	if e != nil {
		return finish(ServiceResult{Domain: domain, State: "dns_error", Message: "DNS роутера: " + redact(e.Error())})
	}
	roots, e := x509.SystemCertPool()
	if e != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	roots.AppendCertsFromPEM(probeRoots)
	proxy, _ := url.Parse("socks5h://127.0.0.1:10808")
	client, tr := newProbeClient(proxy, roots)
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	req.Header.Set("User-Agent", "DARKLS-VPN-Diagnostics/1.1.1")
	resp, e := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	return finish(classifyServiceResponse(domain, resp, e))
}
func (s *Server) serviceProbe(w http.ResponseWriter, r *http.Request) {
	if !s.diagnosticMu.TryLock() {
		fail(w, 409, errors.New("диагностика уже выполняется"))
		return
	}
	defer s.diagnosticMu.Unlock()
	var req struct {
		Service string `json:"service"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	domain, ok := serviceDomains[req.Service]
	if !ok {
		fail(w, 400, errors.New("выберите сервис из списка"))
		return
	}
	if s.disabled() {
		fail(w, 409, errors.New("VPN выключен"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result := checkService(ctx, domain)
	appendEvent("service_probe", domain+": "+result.State)
	respond(w, 200, result)
}
