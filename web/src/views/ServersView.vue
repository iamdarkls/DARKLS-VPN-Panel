<script setup lang="ts">
import { ref, reactive, onUnmounted } from 'vue';
import { api } from '../api';
import type { Connections, ServerItem, Profile } from '../types';
import { cleanDraft, refreshStatus } from '../store';
import { clone, notice, stamp, useAction } from '../composables/ui';
import { fieldLabels, emptyFields, unpack, pack, fieldVisible } from '../outbound';
import type { Outbound, Field } from '../outbound';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import Modal from '../components/Modal.vue';
import ActionButton from '../components/ActionButton.vue';
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
function clearEditor() {
  base.value = undefined;
  id.value = '';
  name.value = '';
  raw.value = '';
  password.value = '';
  Object.assign(fields, emptyFields());
}
function closeEditor() {
  if (pending.value) return;
  editorOpen.value = false;
  clearEditor();
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
}
async function request<T>(path: string, body: Record<string, unknown>): Promise<T> {
  if (pending.value) throw new Error('Проверка сервера ещё выполняется.');
  if (!password.value) throw new Error('Введи пароль панели для проверки или сохранения сервера.');
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
  await request('connection-save', { id: id.value, name: name.value.trim(), outbound: payload() });
  closeEditor();
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
  closeEditor();
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
  if (!confirm('Проверить выбранный канал и переключить? VPN-соединения переподключатся.')) return;
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
defineProps<{ tab: string }>();
defineExpose({ refresh });
</script>
<template>
  <section id="servers-view">
    <div
      id="servers-main"
      v-show="tab === 'main'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-servers-main"
      tabindex="0"
    >
      <PanelBox
        title="Текущее подключение"
        class="current-connection"
        :description="
          (data.profile?.active === 'backup'
            ? 'Выбран резервный профиль. '
            : 'Выбран основной профиль. ') + 'Сохранение копии не меняет работающий VPN.'
        "
      >
        <Facts
          :rows="[
            ['Протокол', data.current?.protocol || 'Не определён'],
            ['Транспорт', data.current?.transport || 'tcp'],
            ['Защита', data.current?.security || 'none'],
            ['Сохранено серверов', (data.items?.length || 0) + ' / ' + (data.limit || 20)],
          ]"
        />
        <button type="button" class="primary" @click="openEditor()">+ Добавить сервер</button>
        <ActionButton
          :action="
            async () =>
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
              )
          "
        >
          Сохранить текущее в список
        </ActionButton>
        <ActionButton
          :disabled="!data.rollback_available"
          :action="
            async () => {
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
          "
        >
          Вернуть предыдущее подключение
        </ActionButton>
      </PanelBox>
      <PanelBox
        title="Мои серверы"
        description="Сохрани сервер после проверки и подключайся отдельной кнопкой. Переименование и удаление копии не меняют работающий VPN."
      >
        <div id="server-list" class="server-list">
          <div v-if="!data.items?.length" class="empty">
            Здесь будут твои серверы. Добавь новый по ссылке или сохрани текущее подключение.
          </div>
          <article
            v-for="item in data.items"
            :key="item.id"
            class="server-card"
            :class="{ 'is-active': item.active }"
            :data-server-id="item.id"
          >
            <div class="server-card-head">
              <h3>{{ item.name }}</h3>
              <span class="route-tag">
                {{ item.active ? 'Подключён' : item.primary ? 'Основной профиль' : 'Сохранён' }}
              </span>
            </div>
            <p class="muted server-meta">
              {{ item.protocol || 'VPN' }} · {{ item.transport || 'tcp' }} ·
              {{ item.security || 'none' }}
            </p>
            <p class="tiny muted">Сохранён: {{ stamp(item.updated_at) }}</p>
            <div class="server-actions">
              <ActionButton
                :kind="item.active ? 'secondary' : 'primary'"
                :disabled="item.active"
                :action="async () => use(item)"
              >
                {{ item.active ? 'Уже подключён' : 'Подключить' }}
              </ActionButton>
              <ActionButton :action="async () => reveal(item)">Изменить</ActionButton>
              <ActionButton :action="async () => rename(item)">Переименовать</ActionButton>
              <ActionButton kind="danger" :action="async () => remove(item)">Удалить</ActionButton>
            </div>
          </article>
        </div>
      </PanelBox>
    </div>
    <div
      id="servers-backup"
      v-show="tab === 'backup'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-servers-backup"
      tabindex="0"
    >
      <PanelBox
        title="Резервный VPN-канал"
        description="Сначала проверим независимый сервер отдельным Xray. Автопереключение по умолчанию выключено; соединения при переключении могут оборваться."
        class="wide"
      >
        <Facts
          :rows="[
            ['Активный канал', profile.active === 'backup' ? 'Резервный' : 'Основной'],
            ['Резерв', profile.backup_configured ? 'Добавлен и проверен' : 'Не настроен'],
            ['Автопереключение', profile.auto_failover ? 'Включено' : 'Выключено'],
            ['Последнее переключение', stamp(profile.last_switch)],
          ]"
        />
        <textarea
          v-model="backupRaw"
          rows="4"
          aria-label="Резервное подключение"
          placeholder="VLESS-ссылка или один outbound JSON"
          autocomplete="off"
          spellcheck="false"
        ></textarea>
        <label>
          Текущий пароль панели
          <input v-model="backupPassword" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="saveBackup">Проверить и сохранить резерв</ActionButton>
        <ActionButton :action="() => switchProfile('primary')">
          Переключиться на основной
        </ActionButton>
        <ActionButton :action="() => switchProfile('backup')">Переключиться на резерв</ActionButton>
      </PanelBox>
    </div>
    <Modal
      class="server-auth-dialog"
      :open="auth.open"
      :pending="auth.pending"
      :label="auth.title"
      @close="closeAuth"
    >
      <form @submit.prevent="run(confirmAuth)">
        <h2 id="server-auth-title">{{ auth.title }}</h2>
        <p class="muted">{{ auth.description }}</p>
        <label v-show="auth.withName">
          Новое название сервера
          <input
            v-model="auth.name"
            maxlength="60"
            :required="auth.withName"
            :disabled="auth.pending"
          />
        </label>
        <label>
          Текущий пароль панели
          <input
            v-model="auth.password"
            type="password"
            autocomplete="current-password"
            required
            :disabled="auth.pending"
          />
        </label>
        <p class="operation-progress" role="status">
          {{ auth.pending ? 'Выполняем действие… Дождись завершения.' : '' }}
        </p>
        <div class="dialog-actions">
          <button type="button" class="secondary" :disabled="auth.pending" @click="closeAuth">
            Отмена
          </button>
          <button class="primary" type="submit" :disabled="auth.pending">{{ auth.label }}</button>
        </div>
      </form>
    </Modal>
    <Modal
      class="connection-dialog"
      :open="editorOpen"
      :pending="pending"
      label="Редактор сервера"
      @close="closeEditor"
    >
      <div class="dialog-heading">
        <h2 id="connection-editor-title">{{ id ? 'Изменить сервер' : 'Новый сервер' }}</h2>
        <button
          class="icon-button"
          type="button"
          aria-label="Закрыть редактор сервера"
          :disabled="pending"
          @click="closeEditor"
        >
          ×
        </button>
      </div>
      <p class="muted">
        Сохранение копии не меняет работающий VPN. Подключение выполняется отдельной кнопкой на
        карточке.
      </p>
      <fieldset :disabled="pending">
        <label>
          Название сервера
          <input v-model="name" maxlength="60" />
        </label>
        <label>
          Способ ввода
          <select v-model="mode">
            <option value="link">Ссылка VLESS или outbound JSON</option>
            <option value="manual">Параметры вручную</option>
          </select>
        </label>
        <div v-show="mode === 'link'">
          <label>
            VLESS-ссылка или outbound JSON
            <textarea v-model="raw" rows="4" spellcheck="false" autocomplete="off"></textarea>
          </label>
          <ActionButton :disabled="pending" :action="parse">Разобрать в ручные поля</ActionButton>
        </div>
        <div v-show="mode === 'manual'" class="connection-fields">
          <div class="settings-grid">
            <template v-for="(label, key) in fieldLabels" :key="key">
              <label v-show="fieldVisible(key, fields)">
                {{ label }}
                <select v-if="selectOptions[key]" v-model="fields[key]" :aria-label="label">
                  <option v-for="[value, text] in selectOptions[key]" :key="value" :value="value">
                    {{ text }}
                  </option>
                </select>
                <textarea
                  v-else-if="key === 'extra'"
                  :aria-label="label"
                  v-model="fields[key]"
                  rows="4"
                  spellcheck="false"
                  autocomplete="off"
                ></textarea>
                <input
                  v-else
                  :aria-label="label"
                  v-model="fields[key]"
                  :type="key === 'port' ? 'number' : 'text'"
                  :min="key === 'port' ? 1 : undefined"
                  :max="key === 'port' ? 65535 : undefined"
                  autocomplete="off"
                />
              </label>
            </template>
          </div>
          <p v-show="fields.network === 'grpc'" class="tiny muted">
            serviceName и режим должны совпадать с сервером. Authority задаётся отдельно от SNI; для
            gRPC через TLS нужен HTTP/2 (h2).
          </p>
        </div>
        <label>
          Пароль для управления подключениями
          <input v-model="password" type="password" autocomplete="current-password" />
        </label>
        <p class="tiny muted">Реквизиты не сохраняются в браузере и очищаются при закрытии окна.</p>
        <div class="editor-actions">
          <ActionButton
            :disabled="pending"
            :action="
              async () => {
                await request('connection-test', { outbound: payload() });
                notice('Изолированная HTTPS-проверка пройдена.');
              }
            "
          >
            Проверить соединение
          </ActionButton>
          <ActionButton kind="primary" :disabled="pending" :action="save">
            Проверить и сохранить сервер
          </ActionButton>
        </div>
        <details class="advanced-apply">
          <summary>Применить без сохранения в список</summary>
          <ActionButton :disabled="pending" :action="apply">Применить как основное</ActionButton>
        </details>
      </fieldset>
      <p class="operation-progress" role="status">
        {{ pending ? 'Проверяем соединение… Дождись завершения.' : '' }}
      </p>
    </Modal>
  </section>
</template>
