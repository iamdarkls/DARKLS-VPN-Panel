package panel

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Reject unknown link options rather than quietly creating a different tunnel.
// XHTTP extra is passed through as JSON, including XMUX and HTTP method settings.
func parseVLESSLink(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "vless" || u.User == nil || u.Hostname() == "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("неверная ссылка VLESS")
	}
	if _, hasPassword := u.User.Password(); hasPassword || u.User.Username() == "" || strings.ContainsAny(u.User.Username(), " \t\r\n") {
		return nil, errors.New("неверный идентификатор VLESS")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("неверный порт VLESS")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("неверное кодирование параметров VLESS")
	}
	allowed := strings.Fields("type security encryption flow fp sni pbk sid spx extra mode path host serviceName authority multiMode alpn allowInsecure x_padding_bytes support-x25519mlkem768 headerType")
	known := map[string]bool{}
	for _, k := range allowed {
		known[k] = true
	}
	for k, v := range q {
		if !known[k] || len(v) != 1 {
			return nil, errors.New("неподдерживаемые или повторяющиеся параметры ссылки; используйте outbound JSON")
		}
	}
	if h := q.Get("headerType"); h != "" && h != "none" {
		return nil, errors.New("TCP headerType требует редактирования outbound JSON")
	}
	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}
	switch network {
	case "tcp", "raw", "xhttp", "ws", "grpc":
	default:
		return nil, errors.New("для этого транспорта используйте outbound JSON")
	}
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	stream := map[string]any{"network": network, "security": security}
	set := func(m map[string]any, key, param string) {
		if q.Get(param) != "" {
			m[key] = q.Get(param)
		}
	}
	switch security {
	case "none":
	case "reality":
		m := map[string]any{}
		for key, param := range map[string]string{"serverName": "sni", "fingerprint": "fp", "publicKey": "pbk", "shortId": "sid", "spiderX": "spx"} {
			set(m, key, param)
		}
		stream["realitySettings"] = m
	case "tls":
		m := map[string]any{}
		set(m, "serverName", "sni")
		set(m, "fingerprint", "fp")
		if q.Get("alpn") != "" {
			m["alpn"] = strings.Split(q.Get("alpn"), ",")
		}
		if q.Get("allowInsecure") != "" {
			v, e := strconv.ParseBool(q.Get("allowInsecure"))
			if e != nil {
				return nil, errors.New("неверный allowInsecure")
			}
			if v {
				m["allowInsecure"] = true
			}
		}
		stream["tlsSettings"] = m
	default:
		return nil, errors.New("неподдерживаемая безопасность транспорта")
	}
	switch network {
	case "xhttp":
		x := map[string]any{}
		set(x, "path", "path")
		set(x, "host", "host")
		set(x, "mode", "mode")
		extra := map[string]any{}
		if q.Get("extra") != "" {
			if json.Unmarshal([]byte(q.Get("extra")), &extra) != nil || extra == nil {
				return nil, errors.New("неверный XHTTP extra JSON")
			}
		}
		if p := q.Get("x_padding_bytes"); p != "" {
			if _, ok := extra["xPaddingBytes"]; !ok {
				extra["xPaddingBytes"] = p
			}
		}
		if len(extra) > 0 {
			x["extra"] = extra
		}
		stream["xhttpSettings"] = x
	case "grpc":
		g := map[string]any{}
		set(g, "serviceName", "serviceName")
		set(g, "authority", "authority")
		mode := q.Get("mode")
		if mode != "" && mode != "gun" && mode != "multi" {
			return nil, errors.New("режим gRPC: gun или multi")
		}
		multi := mode == "multi"
		if raw := q.Get("multiMode"); raw != "" {
			v, e := strconv.ParseBool(raw)
			if e != nil {
				return nil, errors.New("неверный multiMode gRPC")
			}
			if mode != "" && v != multi {
				return nil, errors.New("mode и multiMode задают разные режимы gRPC")
			}
			multi = v
		}
		g["multiMode"] = multi
		stream["grpcSettings"] = g
	case "ws":
		stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]any{"Host": q.Get("host")}}
	}
	encryption := q.Get("encryption")
	if encryption == "" {
		encryption = "none"
	}
	user := map[string]any{"id": u.User.Username(), "encryption": encryption}
	set(user, "flow", "flow")
	return map[string]any{"tag": "vpn", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{user}}}}, "streamSettings": stream}, nil
}
