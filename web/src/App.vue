<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue';
import { api } from './api';
import {
  auth,
  dirty,
  busy,
  status,
  preview,
  login,
  logoutLocal,
  initialize,
  refreshStatus,
  prepareApply,
  cancelApply,
  apply,
  discard,
  setPower,
} from './store';
import { theme } from './composables/theme';
import { useAction, perform, errorMessage, stamp, usePolling } from './composables/ui';
import { views, menus, tabs } from './navigation';
import type { View } from './navigation';
import PanelBox from './components/PanelBox.vue';
import Facts from './components/Facts.vue';
import Modal from './components/Modal.vue';
import Tabs from './components/Tabs.vue';
import Notifications from './components/Notifications.vue';
import SitesView from './views/SitesView.vue';
import DevicesView from './views/DevicesView.vue';
import ServersView from './views/ServersView.vue';
import DiagnosticsView from './views/DiagnosticsView.vue';
import StabilityView from './views/StabilityView.vue';
import TuningView from './views/TuningView.vue';
import UpdatesView from './views/UpdatesView.vue';
import BackupsView from './views/BackupsView.vue';
import ResourcesView from './views/ResourcesView.vue';
import PasswordView from './views/PasswordView.vue';
import NavIcon from './components/NavIcon.vue';
import LogsDialog from './components/LogsDialog.vue';
const logsOpen = ref(false),
  collapsed = ref(false),
  menuSearch = ref('');
try {
  collapsed.value = localStorage.getItem('darkls-sidebar-collapsed') === '1';
} catch {}
watch(collapsed, (value) => {
  try {
    localStorage.setItem('darkls-sidebar-collapsed', value ? '1' : '0');
  } catch {}
});
watch(
  () => auth.loggedIn,
  (value) => {
    if (!value) logsOpen.value = false;
  }
);
const filteredMenus = computed(() =>
  menus
    .map(
      ([name, items]) =>
        [
          name,
          items.filter((id) =>
            views[id].title.toLowerCase().includes(menuSearch.value.toLowerCase())
          ),
        ] as [string, View[]]
    )
    .filter(([, items]) => items.length)
);
function navigate(id: View) {
  if (id === 'logs') logsOpen.value = true;
  else view.value = id;
}
const view = ref<View>('sites'),
  selected = reactive<Record<string, string>>({
    servers: 'main',
    stability: 'history',
    updates: 'panel',
    backups: 'history',
  }),
  user = ref('admin'),
  password = ref(''),
  loginPending = ref(false),
  navigationBusy = ref(false),
  savebar = ref<HTMLElement>();
const { pending, run } = useAction();
type Screen = { refresh: (tab?: string) => Promise<unknown> };
const screens: Partial<Record<View, Screen | null>> = {};
let refreshAgain = false,
  resize: ResizeObserver | undefined,
  stopResizeWatch: (() => void) | undefined;
function screenRef(name: View, element: unknown) {
  screens[name] = element as Screen | null;
}
async function refresh() {
  if (!auth.loggedIn) return;
  if (navigationBusy.value) {
    refreshAgain = true;
    return;
  }
  navigationBusy.value = true;
  try {
    const destination = view.value,
      tab = selected[destination];
    await screens[destination]?.refresh(destination === 'logs' ? 'logs' : tab);
  } catch (e) {
    await perform(async () => {
      throw e;
    });
  } finally {
    navigationBusy.value = false;
    if (refreshAgain) {
      refreshAgain = false;
      if (auth.loggedIn) await refresh();
    }
  }
}
watch([view, () => selected[view.value]], () => {
  void refresh();
});
async function submitLogin() {
  if (loginPending.value) return;
  loginPending.value = true;
  try {
    await login(user.value, password.value);
    password.value = '';
    await nextTick();
    await refresh();
  } catch (e) {
    auth.error = errorMessage(e);
  } finally {
    password.value = '';
    loginPending.value = false;
  }
}
async function logout() {
  if (dirty.value && !confirm('Выйти без сохранения?')) return;
  try {
    await api('logout', 'POST', {});
  } finally {
    logoutLocal();
    password.value = '';
  }
}
const checks = computed(() => {
  const s = status.value;
  return [
    ['xray', 'Xray'],
    ['dns', 'DNS'],
    ['nat', 'TCP'],
    ['udp', 'UDP'],
    ['watch', 'Автоконтроль'],
  ]
    .map(([key, label]) => ({ label, ok: !!s[key as keyof typeof s] }))
    .concat(s.monitor ? [{ label: 'Цикл контроля', ok: s.monitor.fresh }] : [], [
      { label: 'HTTPS', ok: !!s.probe_fresh && !!s.probe?.ok },
    ]);
});
const description = computed(() =>
  status.value.active
    ? 'Последняя успешная HTTPS-проверка: ' + stamp(status.value.probe?.last_success)
    : status.value.disabled
      ? 'Сайты открываются через обычное подключение.'
      : status.value.recovery_confirmation?.error ||
        status.value.probe?.error ||
        'Ожидаем свежую HTTPS-проверку автоконтроля.'
);
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value) {
    event.preventDefault();
    event.returnValue = '';
  }
}
function visible() {
  if (!document.hidden && auth.loggedIn) {
    void perform(refreshStatus);
    void refresh();
  }
}
usePolling(async () => {
  if (auth.loggedIn && !busy.value) {
    await refreshStatus();
    if (['devices', 'metrics', 'logs'].includes(view.value)) await refresh();
  }
}, 30000);
onMounted(async () => {
  window.addEventListener('beforeunload', beforeUnload);
  document.addEventListener('visibilitychange', visible);
  await initialize();
  await nextTick();
  if (auth.loggedIn) await refresh();
  resize = new ResizeObserver(() => {
    document.documentElement.style.setProperty(
      '--savebar-space',
      (savebar.value?.offsetHeight || 0) + 'px'
    );
  });
  stopResizeWatch = watch(
    savebar,
    (element) => {
      resize?.disconnect();
      if (element) resize?.observe(element);
    },
    { immediate: true }
  );
});
onUnmounted(() => {
  window.removeEventListener('beforeunload', beforeUnload);
  document.removeEventListener('visibilitychange', visible);
  stopResizeWatch?.();
  resize?.disconnect();
});
</script>
<template>
  <div class="appearance">
    <label for="theme-select">Тема</label>
    <select id="theme-select" v-model="theme" aria-label="Тема оформления">
      <option value="auto">Как в системе</option>
      <option value="light">Светлая</option>
      <option value="dark">Тёмная</option>
    </select>
  </div>
  <section v-if="!auth.loggedIn" id="login" class="login">
    <form id="login-form" class="login-card" @submit.prevent="submitLogin">
      <div class="brand">
        D
        <span>↗</span>
      </div>
      <p class="eyebrow">ДОМАШНЯЯ СЕТЬ</p>
      <h1>
        Сеть под
        <br />
        контролем.
      </h1>
      <p class="muted">Панель DARKLS VPN для ASUS</p>
      <label>
        Логин
        <input v-model="user" name="user" autocomplete="username" required />
      </label>
      <label>
        Пароль
        <input
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
        />
      </label>
      <button class="primary" type="submit" :disabled="loginPending || !auth.ready">
        Войти в панель
        <span>→</span>
      </button>
      <p id="login-error" class="error" role="alert">{{ auth.error }}</p>
      <p class="tiny muted">Адрес панели: 192.168.50.1:8088</p>
    </form>
  </section>
  <div v-else id="app" :key="auth.epoch" class="app" :class="{ 'sidebar-collapsed': collapsed }">
    <aside>
      <div class="sidebar-brand">
        <button
          class="icon-button collapse-menu"
          :aria-expanded="!collapsed"
          aria-label="Свернуть или развернуть меню"
          @click="collapsed = !collapsed"
        >
          <NavIcon name="menu" />
        </button>
        <a class="wordmark" href="/">
          DARKLS
          <span>VPN</span>
        </a>
      </div>
      <div class="router-label">ASUS TUF-BE6500</div>
      <input
        v-model="menuSearch"
        class="menu-search"
        placeholder="Найти раздел…"
        aria-label="Поиск раздела"
      />
      <nav aria-label="Разделы панели">
        <div v-for="[group, items] in filteredMenus" :key="group" class="nav-group">
          <p class="nav-heading">{{ group }}</p>
          <button
            v-for="id in items"
            :key="id"
            class="nav"
            :class="{ active: view === id }"
            :data-view="id"
            :aria-current="view === id ? 'page' : undefined"
            @click="navigate(id)"
            :aria-label="views[id].title"
            :title="views[id].title"
          >
            <NavIcon :name="id" />
            <span class="nav-label">{{ views[id].title }}</span>
          </button>
        </div>
      </nav>
      <div class="aside-bottom">
        <span class="mini-dot"></span>
        Локальная панель
        <button id="logout" class="link-button" @click="run(logout)">Выйти ↗</button>
        <span id="version" class="tiny">v{{ auth.version }}</span>
      </div>
    </aside>
    <main :aria-busy="navigationBusy">
      <header>
        <div>
          <p class="eyebrow">NETWORK CONTROL</p>
          <h1 id="page-title">{{ views[view].title }}</h1>
          <p id="page-subtitle" class="muted">{{ views[view].subtitle }}</p>
        </div>
        <div id="connection" class="status-pill" :class="{ off: !status.active }">
          {{
            status.active
              ? '● VPN работает'
              : status.disabled
                ? '○ VPN выключен'
                : '◌ Требуется проверка'
          }}
        </div>
        <div class="header-actions">
          <button id="open-logs" class="secondary" @click="logsOpen = true">
            <NavIcon name="logs" />
            Журналы
          </button>
          <button class="secondary" @click="view = 'metrics'">
            <NavIcon name="metrics" />
            Ресурсы
          </button>
          <Notifications />
        </div>
      </header>
      <PanelBox
        v-show="status.recovery_pending"
        id="recovery-warning"
        title="Нужно завершить восстановление"
        role="status"
        :class="{ hidden: !status.recovery_pending }"
      >
        <p>
          Изменение настроек временно заблокировано. Подключи флешку с резервной копией и открой
          Контроль связи → Восстановление. Диагностика и журналы доступны.
        </p>
      </PanelBox>
      <section class="health">
        <div>
          <span id="vpn-light" class="big-dot" :class="{ on: status.active }"></span>
          <div>
            <strong id="vpn-title">
              {{
                status.active
                  ? 'Соединение через VPN подтверждено'
                  : status.disabled
                    ? 'Маршрутизация выключена'
                    : status.routes
                      ? 'Маршруты подняты, проверяем соединение'
                      : 'Маршрутизация восстанавливается'
              }}
            </strong>
            <p id="vpn-desc" class="muted">{{ description }}</p>
          </div>
        </div>
        <button id="power" class="secondary" :disabled="busy" @click="run(setPower)">
          {{ status.disabled ? 'Включить VPN' : 'Выключить VPN' }}
        </button>
        <div id="checks" class="checks">
          <span
            v-for="check in checks"
            :key="check.label"
            class="check"
            :class="{ bad: !check.ok }"
          >
            {{ check.ok ? '✓ ' : '○ ' }}{{ check.label }}
          </span>
        </div>
      </section>
      <div id="notice" class="notice hidden" role="status"></div>
      <Tabs
        v-if="tabs[view]"
        :group="view"
        :items="tabs[view]!"
        :model-value="selected[view]!"
        @update:model-value="selected[view] = $event"
      />
      <SitesView :ref="(el) => screenRef('sites', el)" v-show="view === 'sites'" />
      <DevicesView :ref="(el) => screenRef('devices', el)" v-show="view === 'devices'" />
      <ServersView
        :ref="(el) => screenRef('servers', el)"
        v-show="view === 'servers'"
        :tab="selected.servers!"
      />
      <DiagnosticsView
        :ref="(el) => screenRef('diagnostics', el)"
        v-show="view === 'diagnostics'"
      />
      <StabilityView
        :ref="(el) => screenRef('stability', el)"
        v-show="view === 'stability'"
        :tab="selected.stability!"
      />
      <TuningView :ref="(el) => screenRef('tuning', el)" v-show="view === 'tuning'" />
      <UpdatesView
        :ref="(el) => screenRef('updates', el)"
        v-show="view === 'updates'"
        :tab="selected.updates!"
      />
      <BackupsView
        :ref="(el) => screenRef('backups', el)"
        v-show="view === 'backups'"
        :tab="selected.backups!"
      />
      <ResourcesView
        :ref="(el) => screenRef('metrics', el)"
        v-show="view === 'metrics'"
        mode="metrics"
      />

      <PasswordView v-show="view === 'settings'" />
    </main>
    <footer id="savebar" ref="savebar" :class="{ 'has-changes': dirty || busy }">
      <div>
        <strong id="save-title">
          {{
            busy
              ? 'Выполняем операцию…'
              : dirty
                ? 'Есть несохранённые изменения'
                : 'Все изменения сохранены'
          }}
        </strong>
        <span id="save-subtitle">
          {{
            busy
              ? 'Дождись завершения.'
              : dirty
                ? 'Они вступят в силу после нажатия «Применить».'
                : 'Настройки действуют для домашней сети.'
          }}
        </span>
      </div>
      <div class="save-actions">
        <button id="discard" class="secondary" :disabled="!dirty || busy" @click="discard">
          Отменить
        </button>
        <button id="apply" class="primary" :disabled="!dirty || busy" @click="run(prepareApply)">
          Применить изменения
          <span>↗</span>
        </button>
      </div>
    </footer>
    <LogsDialog v-if="logsOpen" @close="logsOpen = false" />
    <Modal :open="!!preview" label="Перед применением" @close="cancelApply">
      <div v-if="preview" class="panel-box">
        <h2>Перед применением</h2>
        <Facts
          :rows="[
            ['Домены добавятся', preview.data.added_domains.join(', ') || 'Нет'],
            ['Домены уберутся', preview.data.removed_domains.join(', ') || 'Нет'],
            ['Группы', preview.data.groups.join('; ') || 'Без изменений'],
            [
              'Режим устройств',
              preview.data.old_device_mode + ' → ' + preview.data.new_device_mode,
            ],
            [
              'Затронутые устройства',
              preview.data.affected_devices || preview.data.selected_devices,
            ],
            ['Перезапуск Xray', preview.data.xray_restart ? 'Да' : 'Не требуется'],
            ['Перезапуск DNS helper', preview.data.dns_reload ? 'Да' : 'Нет'],
          ]"
        />
        <p class="muted">{{ preview.data.note }}</p>
        <button class="secondary" @click="cancelApply">Вернуться к редактированию</button>
        <button
          class="primary"
          :disabled="pending"
          @click="
            run(async () => {
              await apply();
              await refresh();
            })
          "
        >
          Применить эти изменения
        </button>
      </div>
    </Modal>
  </div>
</template>
