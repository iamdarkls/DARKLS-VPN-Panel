import { registerUnsaved } from '../stores/forms';
import { computed, reactive, ref } from 'vue';
import { api } from '../api';
import type { ServiceHistory, Preferences, Profile } from '../types';
import { activity, loadActivity, refreshStatus, status } from '../stores/vpn';
import { loadState, cleanDraft } from '../stores/draft';
import { notice, stamp, duration, serviceLabel } from './ui';

/** Monitor preferences and guarded recovery; offline recovery must leave VPN explicitly disabled.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useStability() {
  const recoveryBusy = ref(false);
  const history = ref<ServiceHistory>({}),
    password = ref(''),
    xrayPassword = ref(''),
    monitorPassword = ref(''),
    backup = ref(false),
    changed = ref(false),
    form = reactive<Preferences>({
      monitor_services: true,
      interval_seconds: 300,
      auto_failover: false,
    });
  const summary = computed(() => {
    const s = activity.value.summary || {};
    return [
      ['Время наблюдения', duration(s.observed_seconds)],
      ['Недоступность в наблюдаемые интервалы', duration(s.unavailable_seconds)],
      ['Нет данных', duration(s.unknown_seconds)],
      ['Эпизоды, начавшиеся за сутки', s.failure_episodes || 0],
      ['Попытки восстановления', s.recoveries || 0],
      ['Последняя проба', !s.probe_fresh ? 'Нет свежей пробы' : s.probe_ok ? 'Успешна' : 'Ошибка'],
      [
        'Пик занятой RAM',
        s.resource_samples ? (Number(s.peak_used_kib) / 1024).toFixed(1) + ' МиБ' : 'Нет замеров',
      ],
      [
        'Пик RSS панели / Xray',
        s.resource_samples
          ? (Number(s.peak_panel_kib) / 1024).toFixed(1) +
            ' / ' +
            (Number(s.peak_xray_kib) / 1024).toFixed(1) +
            ' МиБ'
          : 'Нет замеров',
      ],
    ] as [string, string | number][];
  });
  const rows = computed(() =>
    Object.values(history.value.current || {}).map((r) => ({
      service: r.service,
      state: serviceLabel(r.state),
      since: stamp(r.since),
      at: stamp(r.at),
      tunnel: r.tunnel_ok == null ? 'Нет свежего замера' : r.tunnel_ok ? 'Работал' : 'Ошибка',
    }))
  );
  async function loadHistory() {
    history.value = await api<ServiceHistory>('service-history');
  }
  async function refresh(tab = 'history') {
    if (tab === 'history') {
      await loadActivity(true);
      await loadHistory();
    } else if (tab === 'monitor') {
      const [p, profiles] = await Promise.all([
        api<Preferences>('preferences'),
        api<Profile>('profiles'),
      ]);
      backup.value = !!profiles.backup_configured;
      if (!changed.value) Object.assign(form, p);
    }
  }
  async function recoverPending(offline = false) {
    if (recoveryBusy.value) return;
    if (
      offline &&
      !confirm(
        'Сохранить резервную копию, выключить VPN и завершить откат файлов без проверки старого сервера? VPN останется выключенным до вашего ручного включения.'
      )
    ) {
      password.value = '';
      return;
    }
    recoveryBusy.value = true;
    try {
      const result = await api<{ backup?: string }>('recovery', 'POST', {
        password: password.value,
        offline,
      });
      notice(
        offline
          ? 'Файлы восстановлены, VPN выключен. Выберите и проверьте рабочий сервер. Копия: panel/recovery-snapshots/' +
              result.backup
          : 'Восстановление завершено'
      );
      await loadState();
    } finally {
      password.value = '';
      await refreshStatus().catch(() => {});
      recoveryBusy.value = false;
    }
  }
  async function repair(component: string) {
    cleanDraft();
    if (
      component === 'xray' &&
      !confirm('Текущие соединения через VPN переподключатся. Продолжить?')
    )
      return;
    try {
      await api('recover-part', 'POST', {
        component,
        ...(component === 'xray' ? { password: xrayPassword.value } : {}),
      });
      notice('Проверка / восстановление выполнены. Подробности в журнале.');
      await refreshStatus();
    } finally {
      xrayPassword.value = '';
    }
  }
  async function save() {
    try {
      await api('preferences', 'POST', {
        password: monitorPassword.value,
        preferences: { ...form },
      });
      changed.value = false;
      await refresh('monitor');
      notice('Настройки контроля сохранены.');
    } finally {
      monitorPassword.value = '';
    }
  }
  registerUnsaved(() => changed.value);
  return {
    summary,
    activity,
    loadHistory,
    rows,
    history,
    stamp,
    serviceLabel,
    password,
    recoveryBusy,
    recoverPending,
    status,
    repair,
    xrayPassword,
    changed,
    form,
    backup,
    monitorPassword,
    save,
    refresh,
  };
}
