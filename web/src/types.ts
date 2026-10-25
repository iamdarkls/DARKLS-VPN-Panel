export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
export type JsonObject = { [key: string]: Json };
export interface DeviceRef {
  mac: string;
  name?: string;
}
export interface Device extends DeviceRef {
  ip?: string;
  online?: boolean;
}
export interface Group {
  id: string;
  name: string;
  domains: string[];
  enabled: boolean;
  category?: string;
  block_quic?: boolean;
  enabled_until?: number;
  timed_previous?: boolean;
  quic_until?: number;
  quic_previous?: boolean;
}
export interface PanelState {
  version: number;
  groups: Group[];
  policy: { mode: 'all' | 'include' | 'exclude'; devices: DeviceRef[] };
}
export interface Probe {
  ok?: boolean;
  last_success?: number;
  last_failure?: number;
  failures?: number;
  recoveries?: number;
  last_recovery?: number;
  recovery_reason?: string;
  duration_ms?: number;
  error?: string;
}
export interface Status {
  active?: boolean;
  disabled?: boolean;
  routes?: boolean;
  nat?: boolean;
  udp?: boolean;
  xray?: boolean;
  dns?: boolean;
  watch?: boolean;
  recovery_pending?: boolean;
  recovery_confirmation?: { ok: boolean; error?: string };
  monitor?: { fresh: boolean };
  probe?: Probe;
  probe_fresh?: boolean;
}
export interface Job {
  id?: string;
  phase?: string;
  message?: string;
  started?: number;
  finished?: number;
}
export interface ReleaseStatus {
  current?: string;
  latest?: string;
  available?: boolean;
  checked_at?: number;
  published_at?: number;
  prerelease?: boolean;
  notes?: string;
  error?: string;
  configured?: boolean;
  signed?: boolean;
}
export interface CoreSettings {
  check: boolean;
  auto_install: boolean;
  hour_msk: number;
  delay_days?: number;
  blocked_versions?: Record<string, number>;
}
export interface CoreResponse {
  status: ReleaseStatus;
  settings: CoreSettings;
  job: Job;
  rollback_available?: boolean;
}
export interface ServerItem {
  id: string;
  name: string;
  protocol?: string;
  transport?: string;
  security?: string;
  updated_at?: number;
  active?: boolean;
  primary?: boolean;
}
export interface Connections {
  items?: ServerItem[];
  current?: { protocol?: string; transport?: string; security?: string };
  rollback_available?: boolean;
  limit?: number;
  profile?: Profile;
}
export interface Profile {
  active?: string;
  backup_configured?: boolean;
  auto_failover?: boolean;
  last_switch?: number;
}
export interface Preferences {
  monitor_services: boolean;
  interval_seconds: number;
  auto_failover: boolean;
}
export interface Storage {
  items?: { id: string; name: string; bytes: number; consequence: string }[];
  cleanup?: { enabled: boolean };
  retention_days?: number;
  last_cleanup?: { at?: number; removed?: number; bytes?: number; error?: string };
}
export interface Preview {
  added_domains: string[];
  removed_domains: string[];
  groups: string[];
  old_device_mode: string;
  new_device_mode: string;
  affected_devices?: string;
  selected_devices?: number;
  xray_restart?: boolean;
  dns_reload?: boolean;
  note?: string;
}
export type Fact = [string, string | number | boolean | null | undefined];
export interface Activity {
  summary?: Record<string, string | number | boolean>;
  notifications?: { id: string; at: number; kind: string; message: string }[];
}
export interface ServiceHistory {
  current?: Record<
    string,
    { service: string; state: string; since: number; at: number; tunnel_ok: boolean | null }
  >;
  events?: {
    previous?: string;
    service: string;
    state: string;
    at: number;
    previous_duration_seconds?: number;
  }[];
  retention?: string;
}
export interface ServiceResult {
  domain: string;
  message: string;
  http?: number;
  duration_ms: number;
}
export interface Connectivity {
  complete?: boolean;
  clock?: boolean;
  xray?: boolean;
  dns_process?: boolean;
  local_dns?: boolean;
  tunnel_dns?: boolean;
  rules?: boolean;
  direct_internet?: boolean;
  server_tcp?: boolean;
  tunnel_https?: boolean;
  traffic_checked?: boolean;
  errors?: Record<string, string>;
}
export interface Dependencies {
  rows: {
    domain: string;
    dns: string;
    configured: boolean;
    groups: string[];
    addresses: string[];
    in_vpn_set: boolean;
    https?: ServiceResult;
  }[];
  https: ServiceResult;
  note: string;
}
export interface Diagnosis {
  domain: string;
  client_ip: string;
  device?: Device;
  policy_mismatch: boolean;
  device_eligible: boolean;
  routes_active: boolean;
  groups: Group[];
  addresses: { ip: string; via_vpn: boolean; reason?: string }[];
}
