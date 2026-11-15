import type { Connectivity, Status } from './types';

export function connectionRows(c?: Connectivity | null, status?: Status): [string, string][] {
  const result = (value?: boolean, checked?: boolean) =>
    value === true ? 'Подтверждено' : checked ? 'Не подтверждено' : 'Нет замера';
  const complete = c?.complete !== false && !!c;
  const latestHTTPS = status?.probe_fresh && (status.probe?.checked_at || 0) >= (c?.at || 0);
  return [
    ['Прямой интернет (HTTPS)', result(c?.direct_internet, complete && c?.wan_checked)],
    ['Доступность VPN-сервера (TCP)', result(c?.server_tcp, complete && c?.server_checked)],
    [
      'HTTPS через VPN',
      latestHTTPS
        ? result(status?.probe?.ok, true)
        : result(c?.tunnel_https, complete && c?.traffic_checked),
    ],
    ['DNS helper', result(c?.local_dns, complete || !!c?.errors?.local_dns)],
    ['DNS Xray', result(c?.tunnel_dns, complete || !!c?.errors?.tunnel_dns)],
    ['Правила роутера', result(c?.rules, complete || !!c?.errors?.rules)],
  ];
}
