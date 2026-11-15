import { computed, ref } from 'vue';
import { connectionRows } from '../connectivity';
import { api } from '../api';
import type { Connectivity, Dependencies, Diagnosis, ServiceResult } from '../types';
import { devices, loadDevices, probeFacts, refreshStatus } from '../stores/vpn';
import { services } from './ui';
import { draft } from '../stores/draft';

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
  const dependencyServices = computed(() =>
    Array.from(
      new Map<string, string>([
        ...services,
        ...(draft.value?.groups || []).map((g) => [g.id, g.name] as const),
      ]).entries()
    )
  );
  const rows = computed(() =>
    connectivity.value
      ? [
          [
            'Диагностика',
            connectivity.value.complete === false
              ? 'Не завершена: часть результатов неизвестна'
              : 'Завершена',
          ] as [string, string],
          ['Часы', connectivity.value.clock ? 'Синхронизированы' : 'Ожидаем NTP'] as [
            string,
            string,
          ],
          ...connectionRows(connectivity.value),
        ]
      : []
  );
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
    dependencyServices,
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
