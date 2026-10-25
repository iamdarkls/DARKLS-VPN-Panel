import { clone } from './composables/ui';
export interface Transport {
  [key: string]: unknown;
  path?: string;
  host?: string;
  headers?: Record<string, string>;
  mode?: string;
  extra?: Record<string, unknown>;
  serviceName?: string;
  authority?: string;
  multiMode?: boolean;
}
interface TLS {
  [key: string]: unknown;
  serverName?: string;
  fingerprint?: string;
  publicKey?: string;
  shortId?: string;
  spiderX?: string;
  alpn?: string[];
}
export interface Outbound {
  [key: string]: unknown;
  protocol: string;
  tag?: string;
  settings?: {
    [key: string]: unknown;
    vnext?: {
      [key: string]: unknown;
      address?: string;
      port?: number;
      users?: { [key: string]: unknown; id?: string; encryption?: string; flow?: string }[];
    }[];
  };
  streamSettings?: {
    [key: string]: unknown;
    network?: string;
    security?: string;
    realitySettings?: TLS;
    tlsSettings?: TLS;
    xhttpSettings?: Transport;
    wsSettings?: Transport;
    grpcSettings?: Transport;
  };
}
export const fieldLabels = {
  address: 'Адрес сервера',
  port: 'Порт сервера',
  id: 'UUID VLESS',
  encryption: 'Encryption',
  flow: 'Flow (можно оставить пустым)',
  sni: 'SNI / serverName',
  fp: 'TLS fingerprint',
  pbk: 'Reality publicKey',
  sid: 'Reality shortId',
  spx: 'Reality spiderX',
  path: 'Путь транспорта',
  host: 'Host транспорта',
  serviceName: 'gRPC serviceName',
  authority: 'gRPC authority (необязательно)',
  alpn: 'TLS ALPN (через запятую)',
  network: 'Транспорт',
  security: 'Защита транспорта',
  mode: 'Режим XHTTP',
  grpcMode: 'Режим gRPC',
  extra: 'XHTTP extra JSON (сохраняет XMUX и дополнительные настройки)',
};
export type Field = keyof typeof fieldLabels;
export type ConnectionFields = Record<Field, string>;
export function emptyFields(): ConnectionFields {
  return {
    address: '',
    port: '443',
    id: '',
    encryption: 'none',
    flow: '',
    sni: '',
    fp: '',
    pbk: '',
    sid: '',
    spx: '',
    path: '',
    host: '',
    serviceName: '',
    authority: '',
    alpn: '',
    network: 'tcp',
    security: 'none',
    mode: 'auto',
    grpcMode: 'gun',
    extra: '{}',
  };
}
export function unpack(out: Outbound): ConnectionFields {
  const st = out.streamSettings || {},
    server = out.settings?.vnext?.[0] || {},
    user = server.users?.[0] || {},
    tls = (st.security === 'reality' ? st.realitySettings : st.tlsSettings) || {},
    transport = (st.network === 'ws' ? st.wsSettings : st.xhttpSettings) || {};
  return {
    address: server.address || '',
    port: String(server.port || 443),
    id: user.id || '',
    encryption: user.encryption || 'none',
    flow: user.flow || '',
    sni: tls.serverName || '',
    fp: tls.fingerprint || '',
    pbk: tls.publicKey || '',
    sid: tls.shortId || '',
    spx: tls.spiderX || '',
    path: transport.path || '',
    host: transport.host || transport.headers?.Host || '',
    serviceName: st.grpcSettings?.serviceName || '',
    authority: st.grpcSettings?.authority || '',
    alpn: (st.tlsSettings?.alpn || []).join(','),
    network: st.network || 'tcp',
    security: st.security || 'none',
    mode: st.xhttpSettings?.mode || 'auto',
    grpcMode: st.grpcSettings?.multiMode ? 'multi' : 'gun',
    extra: JSON.stringify(st.xhttpSettings?.extra || {}, null, 2),
  };
}
// Clone the revealed outbound and update known fields only. Unfamiliar XHTTP,
// gRPC and sockopt settings survive round-trips through the manual editor.
export function pack(base: Outbound | undefined, fields: ConnectionFields): string {
  const out = base ? clone(base) : ({ protocol: 'vless' } as Outbound);
  if (out.protocol !== 'vless') throw new Error('Для этого протокола используй outbound JSON.');
  if (
    !fields.address.trim() ||
    !fields.id.trim() ||
    !Number.isInteger(Number(fields.port)) ||
    +fields.port < 1 ||
    +fields.port > 65535
  )
    throw new Error('Укажи адрес, порт 1–65535 и UUID.');
  out.tag = 'vpn';
  out.settings ||= {};
  out.settings.vnext ||= [{}];
  const server = (out.settings.vnext[0] ||= {});
  server.address = fields.address.trim();
  server.port = +fields.port;
  server.users ||= [{}];
  const user = (server.users[0] ||= {});
  user.id = fields.id.trim();
  user.encryption = fields.encryption.trim() || 'none';
  if (fields.flow.trim()) user.flow = fields.flow.trim();
  else delete user.flow;
  const st = (out.streamSettings ||= {});
  st.network = fields.network;
  st.security = fields.security;
  if (st.security !== 'none') {
    const key = st.security === 'reality' ? 'realitySettings' : 'tlsSettings',
      tls = (st[key] ||= {});
    tls.serverName = fields.sni.trim();
    tls.fingerprint = fields.fp.trim();
    if (st.security === 'tls') {
      const alpn = fields.alpn
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      if (alpn.length) tls.alpn = alpn;
      else delete tls.alpn;
    }
    if (st.security === 'reality')
      Object.assign(tls, {
        publicKey: fields.pbk.trim(),
        shortId: fields.sid.trim(),
        spiderX: fields.spx.trim(),
      });
  }
  if (st.network === 'xhttp') {
    let extra: unknown;
    try {
      extra = JSON.parse(fields.extra || '{}');
    } catch {
      throw new Error('XHTTP extra должен быть JSON-объектом.');
    }
    if (!extra || Array.isArray(extra) || typeof extra !== 'object')
      throw new Error('XHTTP extra должен быть JSON-объектом.');
    Object.assign((st.xhttpSettings ||= {}), {
      path: fields.path,
      host: fields.host,
      mode: fields.mode,
      extra,
    });
  }
  if (st.network === 'ws') {
    const ws = (st.wsSettings ||= {});
    ws.path = fields.path;
    (ws.headers ||= {}).Host = fields.host;
  }
  if (st.network === 'grpc')
    Object.assign((st.grpcSettings ||= {}), {
      serviceName: fields.serviceName,
      authority: fields.authority,
      multiMode: fields.grpcMode === 'multi',
    });
  return JSON.stringify(out);
}
export function fieldVisible(key: Field, f: ConnectionFields) {
  if (['sni', 'fp'].includes(key)) return f.security !== 'none';
  if (['pbk', 'sid', 'spx'].includes(key)) return f.security === 'reality';
  if (['path', 'host'].includes(key)) return ['xhttp', 'ws'].includes(f.network);
  if (['serviceName', 'authority', 'grpcMode'].includes(key)) return f.network === 'grpc';
  if (key === 'alpn') return f.security === 'tls';
  if (['mode', 'extra'].includes(key)) return f.network === 'xhttp';
  return true;
}
