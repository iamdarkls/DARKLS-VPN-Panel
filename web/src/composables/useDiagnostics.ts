import { computed, ref } from 'vue';
import { api } from '../api';
import type { Connectivity, Dependencies, Diagnosis, ServiceResult } from '../types';
import { devices, loadDevices, probeFacts, refreshStatus } from '../store';
import { services } from './ui';

/** Explicit diagnostic actions and their results; a service failure never restarts the tunnel.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useDiagnostics() {
  const service = ref('chatgpt'),
    serviceResult = ref<ServiceResult>(),
    domain = ref(''),
    device = ref(''),
    diagnosis = ref<Diagnosis>(),
    connectivity = ref<Connectivity>(),
    dependencyService = ref('instagram'),
    dependencies = ref<Dependencies>();
  const rows = computed(() => {
    const c = connectivity.value;
    if (!c) return [];
    return [
      [
        'Диагностика',
        c.complete === false ? 'Не завершена: часть результатов неизвестна' : 'Завершена',
      ],
      ['Часы', c.clock ? 'Синхронизированы' : 'Ожидаем NTP'],
      ['Xray', c.xray],
      ['Процесс DNS helper', c.dns_process],
      ['DNS helper', c.local_dns],
      ['DNS Xray', c.tunnel_dns],
      ['Правила', c.rules],
      ['Прямой HTTPS', c.traffic_checked ? c.direct_internet : 'Не проверен'],
      ['TCP VPN-сервера', c.traffic_checked ? c.server_tcp : 'Не проверен'],
      ['HTTPS через VPN', c.traffic_checked ? c.tunnel_https : 'Не проверен'],
    ].map(
      ([k, v]) =>
        [
          String(k),
          typeof v === 'boolean'
            ? v
              ? 'Ответил / исправен'
              : 'Не подтверждён'
            : String(v ?? 'Не проверен'),
        ] as [string, string]
    );
  });
  const dependencyRows = computed(
    () =>
      dependencies.value?.rows.map((r) => ({
        domain: r.domain,
        dns: r.dns,
        route: r.configured
          ? 'Выбран группой: ' + r.groups.join(', ')
          : 'Нет включённого доменного правила',
        ip: r.addresses.join(', ') || 'Нет A-записи',
        set: r.in_vpn_set ? 'Да' : 'Нет',
        https: r.https?.message || 'Не проверен',
      })) || []
  );
  async function diagnose() {
    diagnosis.value = await api<Diagnosis>('diagnose', 'POST', {
      domain: domain.value,
      device_mac: device.value,
    });
  }
  async function probeService() {
    serviceResult.value = await api<ServiceResult>('service-probe', 'POST', {
      service: service.value,
    });
  }
  async function probeTunnel() {
    await api('probe', 'POST', {});
    await refreshStatus();
  }
  async function checkConnectivity() {
    connectivity.value = await api<Connectivity>('connectivity', 'POST', {});
  }
  async function checkDependencies() {
    dependencies.value = await api<Dependencies>('dependencies', 'POST', {
      service: dependencyService.value,
    });
  }
  return {
    checkDependencies,
    checkConnectivity,
    probeTunnel,
    probeService,
    service,
    services,
    serviceResult,
    probeFacts,
    device,
    devices,
    domain,
    diagnose,
    diagnosis,
    connectivity,
    rows,
    dependencyService,
    dependencies,
    dependencyRows,
    loadDevices,
  };
}
