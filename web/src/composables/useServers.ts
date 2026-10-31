import { useUnsavedForm } from './useUnsavedForm';
import { registerUnsaved } from '../stores/forms';
import { ref, reactive, onUnmounted } from 'vue';
import { api } from '../api';
import type { Connections, ServerItem, Profile } from '../types';
import { cleanDraft } from '../stores/draft';
import { refreshStatus } from '../stores/vpn';
import { clone, notice, stamp, useAction } from './ui';
import { fieldLabels, emptyFields, unpack, pack, fieldVisible } from '../outbound';
import type { Outbound, Field } from '../outbound';

/** Saved-server editor and password confirmations; preserve unknown outbound fields and clear secrets on disposal.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useServers() {
  const data = ref<Connections>({}),
    profile = ref<Profile>({}),
    editorOpen = ref(false),
    base = ref<Outbound>(),
    id = ref(''),
    name = ref(''),
    raw = ref(''),
    mode = ref('link'),
    password = ref(''),
    fields = reactive(emptyFields()),
    pending = ref(false);
  const { run } = useAction();
  const auth = reactive({
    open: false,
    title: '',
    description: '',
    label: '',
    name: '',
    withName: false,
    password: '',
    pending: false,
  });
  let authorized: ((body: Record<string, string>) => Promise<unknown>) | undefined,
    after: (() => Promise<void>) | undefined;
  const backupRaw = ref(''),
    backupPassword = ref('');
  const selectOptions: Partial<Record<Field, string[][]>> = {
    network: ['tcp', 'raw', 'xhttp', 'ws', 'grpc'].map((x) => [x, x]),
    security: ['none', 'reality', 'tls'].map((x) => [x, x]),
    mode: ['auto', 'packet-up', 'stream-up', 'stream-one'].map((x) => [x, x]),
    grpcMode: [
      ['gun', 'Gun — обычный'],
      ['multi', 'Multi — многопоточный'],
    ],
  };
  const editorGuard = useUnsavedForm(
    () => ({ name: name.value, raw: raw.value, mode: mode.value, fields: { ...fields } }),
    () => editorOpen.value
  );
  registerUnsaved(() => !!backupRaw.value.trim());
  function clearEditor() {
    base.value = undefined;
    id.value = '';
    name.value = '';
    raw.value = '';
    password.value = '';
    Object.assign(fields, emptyFields());
  }
  function closeEditor() {
    if (pending.value || !editorGuard.confirmDiscard()) return;
    finishEditor();
  }
  function finishEditor() {
    editorOpen.value = false;
    clearEditor();
    editorGuard.forget();
  }
  function openEditor(item?: ServerItem, out?: Outbound) {
    clearEditor();
    id.value = item?.id || '';
    name.value = item?.name || '';
    mode.value = 'link';
    if (out) {
      base.value = clone(out);
      raw.value = JSON.stringify(out, null, 2);
      Object.assign(fields, unpack(out));
      mode.value =
        out.protocol === 'vless' &&
        ['tcp', 'raw', 'xhttp', 'ws', 'grpc'].includes(out.streamSettings?.network || 'tcp')
          ? 'manual'
          : 'link';
    }
    editorOpen.value = true;
    editorGuard.markClean();
  }
  async function request<T>(path: string, body: Record<string, unknown>): Promise<T> {
    if (pending.value) throw new Error('Проверка сервера ещё выполняется.');
    if (!password.value)
      throw new Error('Введи пароль панели для проверки или сохранения сервера.');
    pending.value = true;
    try {
      return await api<T>(path, 'POST', { password: password.value, ...body });
    } finally {
      password.value = '';
      pending.value = false;
    }
  }
  function payload() {
    if (mode.value === 'manual') return pack(base.value, fields);
    if (!raw.value.trim()) throw new Error('Вставь ссылку или outbound JSON.');
    return raw.value.trim();
  }
  async function parse() {
    const d = await request<{ outbound: Outbound }>('connection-parse', { outbound: raw.value });
    base.value = clone(d.outbound);
    raw.value = JSON.stringify(d.outbound, null, 2);
    Object.assign(fields, unpack(d.outbound));
    mode.value = d.outbound.protocol === 'vless' ? 'manual' : 'link';
    notice('Параметры разобраны. Сервер ещё не сохранён.');
  }
  async function save() {
    if (!name.value.trim()) throw new Error('Укажи название сервера.');
    await request('connection-save', {
      id: id.value,
      name: name.value.trim(),
      outbound: payload(),
    });
    finishEditor();
    await load();
    notice('Сервер сохранён. Для переключения нажми «Подключить» на его карточке.');
  }
  async function apply() {
    cleanDraft();
    if (
      !confirm(
        'Проверить и заменить основной сервер без сохранения копии в список? Соединения переподключатся.'
      )
    )
      return;
    await request('connection-apply', { outbound: payload() });
    finishEditor();
    await load();
    await refreshStatus();
    notice('Основное подключение изменено и проверено.');
  }
  function authorize(
    title: string,
    description: string,
    label: string,
    path: string,
    body: Record<string, string>,
    done?: () => Promise<void>,
    defaultName?: string
  ) {
    if (auth.open) throw new Error('Сначала заверши открытое действие.');
    Object.assign(auth, {
      open: true,
      title,
      description,
      label,
      name: defaultName || '',
      withName: defaultName !== undefined,
      password: '',
      pending: false,
    });
    authorized = (values) => api(path, 'POST', { ...body, ...values });
    after = done;
  }
  function closeAuth() {
    if (auth.pending) return;
    auth.open = false;
    auth.password = '';
    auth.name = '';
    authorized = undefined;
    after = undefined;
  }
  async function confirmAuth() {
    if (auth.pending || !authorized) return;
    auth.pending = true;
    try {
      const result = await authorized({
        password: auth.password,
        ...(auth.withName ? { name: auth.name.trim() } : {}),
      });
      const callback = after;
      auth.pending = false;
      closeAuth();
      if (callback) await callback();
      return result;
    } finally {
      auth.password = '';
      auth.pending = false;
    }
  }
  function use(item: ServerItem) {
    cleanDraft();
    authorize(
      'Подключить «' + item.name + '»',
      'Проверим сервер и переключим основной VPN. При ошибке вернём прежний канал.',
      'Проверить и подключить',
      'connection-use',
      { id: item.id },
      async () => {
        await load();
        await refreshStatus();
        notice('Сервер «' + item.name + '» подключён.');
      }
    );
  }
  function reveal(item: ServerItem) {
    authorize(
      'Изменить «' + item.name + '»',
      'Пароль нужен, чтобы открыть реквизиты этого сервера.',
      'Открыть параметры',
      'connection-reveal',
      { id: item.id }
    );
    authorized = async (values) => {
      const result = await api<{ outbound: Outbound }>('connection-reveal', 'POST', {
        id: item.id,
        ...values,
      });
      after = async () => openEditor(item, result.outbound);
      return result;
    };
  }
  function rename(item: ServerItem) {
    authorize(
      'Переименовать сервер',
      'Меняется только название в списке. VPN не переподключится.',
      'Сохранить название',
      'connection-rename',
      { id: item.id },
      async () => {
        await load();
        notice('Название сервера сохранено.');
      },
      item.name
    );
  }
  function remove(item: ServerItem) {
    authorize(
      'Удалить «' + item.name + '»?',
      'Удалится только копия из списка. Работающее подключение останется в конфигурации роутера.',
      'Удалить копию',
      'connection-delete',
      { id: item.id },
      async () => {
        await load();
        notice('Сохранённая копия удалена. Рабочее подключение не изменено.');
      }
    );
  }
  async function load() {
    data.value = await api<Connections>('connections');
  }
  async function refresh(tab = 'main') {
    if (tab === 'backup') profile.value = await api<Profile>('profiles');
    else await load();
  }
  async function saveBackup() {
    cleanDraft();
    try {
      await api('profile-save', 'POST', {
        password: backupPassword.value,
        outbound: backupRaw.value,
      });
      backupRaw.value = '';
      notice('Резерв прошёл HTTPS-проверку и сохранён.');
      await refresh('backup');
    } finally {
      backupPassword.value = '';
    }
  }
  async function switchProfile(selected: string) {
    cleanDraft();
    if (!confirm('Проверить выбранный канал и переключить? VPN-соединения переподключатся.'))
      return;
    try {
      await api('profile-switch', 'POST', { profile: selected, password: backupPassword.value });
      notice('Канал переключён.');
      await refresh('backup');
      await refreshStatus();
    } finally {
      backupPassword.value = '';
    }
  }
  onUnmounted(() => {
    clearEditor();
    auth.password = '';
    backupRaw.value = '';
    backupPassword.value = '';
    authorized = undefined;
    after = undefined;
  });
  async function snapshot() {
    authorize(
      'Сохранить текущее подключение',
      'Создадим проверенную копию текущего сервера. Работающий VPN не изменится.',
      'Сохранить копию',
      'connection-snapshot',
      {},
      async () => {
        await load();
        notice('Текущее подключение сохранено в списке.');
      },
      'Текущее подключение'
    );
  }
  async function rollback() {
    cleanDraft();
    authorize(
      'Вернуть предыдущее подключение',
      'Проверим прежний сервер и переключим VPN.',
      'Проверить и вернуть',
      'connection-rollback',
      {},
      async () => {
        await load();
        await refreshStatus();
        notice('Предыдущее подключение восстановлено.');
      }
    );
  }
  async function testConnection() {
    await request('connection-test', { outbound: payload() });
    notice('Изолированная HTTPS-проверка пройдена.');
  }
  return {
    testConnection,
    rollback,
    snapshot,
    data,
    openEditor,
    stamp,
    use,
    reveal,
    rename,
    remove,
    profile,
    backupRaw,
    backupPassword,
    saveBackup,
    switchProfile,
    auth,
    closeAuth,
    run,
    confirmAuth,
    editorOpen,
    pending,
    closeEditor,
    id,
    name,
    mode,
    raw,
    parse,
    fieldLabels,
    selectOptions,
    fields,
    fieldVisible,
    password,
    save,
    apply,
    refresh,
  };
}
