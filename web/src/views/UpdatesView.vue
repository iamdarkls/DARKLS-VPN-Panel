<script setup lang="ts">
import { computed, ref } from 'vue';
import { api } from '../api';
import type { ReleaseStatus, Job } from '../types';
import { busy, cleanDraft, waitForPanelUpdate } from '../store';
import { stamp, useAction } from '../composables/ui';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';
import CoreUpdates from './CoreUpdates.vue';
defineProps<{ tab: string }>();
const github = ref<ReleaseStatus>({}),
  githubPassword = ref(''),
  githubProgress = ref(''),
  uploadPassword = ref(''),
  uploadFile = ref<File>(),
  uploadProgress = ref(''),
  core = ref<InstanceType<typeof CoreUpdates>>();
const { pending, run } = useAction();
const latest = computed(() =>
  github.value.available && github.value.signed ? github.value.latest : ''
);
async function load() {
  github.value = await api<ReleaseStatus>('github');
}
async function install() {
  cleanDraft();
  const version = latest.value;
  if (!version) throw new Error('Сначала проверь обновления.');
  busy.value = true;
  githubProgress.value = 'Загружаем выпуск и проверяем подпись…';
  try {
    const d = await api<{ job: Job }>('github-install', 'POST', {
      password: githubPassword.value,
      version,
    });
    githubPassword.value = '';
    await waitForPanelUpdate(d.job, (m) => (githubProgress.value = m));
  } finally {
    githubPassword.value = '';
    busy.value = false;
  }
}
async function upload() {
  cleanDraft();
  if (!uploadFile.value || uploadFile.value.size > 8 * 1024 * 1024)
    throw new Error('Выбери пакет ZIP размером до 8 МиБ.');
  const form = new FormData();
  form.append('password', uploadPassword.value);
  form.append('package', uploadFile.value);
  busy.value = true;
  uploadProgress.value = 'Загружаем и проверяем пакет…';
  let accepted = false;
  try {
    const d = await api<{ job: Job }>('update', 'POST', form, true);
    accepted = true;
    uploadPassword.value = '';
    await waitForPanelUpdate(d.job, (m) => (uploadProgress.value = m));
  } finally {
    uploadPassword.value = '';
    busy.value = false;
    if (!accepted) uploadProgress.value = 'Пакет не установлен.';
  }
}
async function refresh(tab = 'panel') {
  if (tab === 'panel') await load();
  else if (tab === 'core') await core.value?.refresh();
}
defineExpose({ refresh });
</script>
<template>
  <section id="updates-view">
    <div
      id="updates-panel"
      v-show="tab === 'panel'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-updates-panel"
      tabindex="0"
    >
      <PanelBox
        title="Панель DARKLS VPN · GitHub"
        class="wide"
        description="Источник: iamdarkls/DARKLS-VPN-Panel. Проверяем раз в сутки; установка только по кнопке. Подпись Ed25519 подтверждает автора пакета. Ядро обновляется во вкладке «Ядро Xray»."
      >
        <ActionButton
          id="github-check"
          :action="
            async () => {
              github = await api<ReleaseStatus>('github-check', 'POST', {});
            }
          "
        >
          Проверить обновления
        </ActionButton>
        <Facts
          id="github-status"
          :rows="[
            ['Установлена', github.current],
            ['Последний выпуск', github.latest || '—'],
            [
              'Подпись',
              github.signed
                ? 'Подтверждена'
                : github.configured
                  ? 'Не проверена'
                  : 'Сначала настройте свой репозиторий',
            ],
            ['Проверено', stamp(github.checked_at)],
            [
              'Состояние',
              github.error ||
                (!github.configured
                  ? 'Первый Release из своего репозитория нужно установить ZIP-файлом.'
                  : !github.checked_at
                    ? 'Проверка ещё не выполнялась'
                    : github.available
                      ? 'Доступно обновление'
                      : 'Обновлений нет'),
            ],
          ]"
        />
        <pre id="github-notes" class="log-box">{{ github.notes || '' }}</pre>
        <form v-show="latest" id="github-install-form" @submit.prevent="run(install)">
          <label>
            Текущий пароль
            <input
              id="github-password"
              v-model="githubPassword"
              type="password"
              autocomplete="current-password"
              required
            />
          </label>
          <button class="primary" type="submit" :disabled="pending || busy">
            Установить проверенный выпуск
          </button>
        </form>
        <p id="github-progress" class="tiny muted" role="status">{{ githubProgress }}</p>
      </PanelBox>
    </div>
    <div
      id="updates-core"
      v-show="tab === 'core'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-updates-core"
      tabindex="0"
    >
      <CoreUpdates ref="core" />
    </div>
    <div
      id="updates-manual"
      v-show="tab === 'manual'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-updates-manual"
      tabindex="0"
    >
      <PanelBox
        title="Установка панели из ZIP"
        class="wide"
        description="Проверим состав, архитектуру и SHA-256 файлов. Пароль и списки сохранятся; при ошибке установщик попробует вернуть предыдущую версию."
      >
        <form id="update-form" @submit.prevent="run(upload)">
          <label>
            Пакет обновления
            <input
              id="update-file"
              type="file"
              accept=".zip,application/zip"
              required
              @change="uploadFile = ($event.target as HTMLInputElement).files?.[0]"
            />
          </label>
          <label>
            Текущий пароль
            <input
              id="update-password"
              v-model="uploadPassword"
              type="password"
              autocomplete="current-password"
              required
            />
          </label>
          <button class="primary" type="submit" :disabled="pending || busy">
            Проверить и установить
          </button>
        </form>
        <p id="update-progress" class="tiny muted" role="status">{{ uploadProgress }}</p>
        <p class="footnote">
          Загружай пакеты из доверенного источника. Во время установки панель кратко перезапустится,
          затем потребуется повторный вход.
        </p>
      </PanelBox>
    </div>
  </section>
</template>
