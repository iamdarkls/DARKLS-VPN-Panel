<script setup lang="ts">
import { ref } from 'vue';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';
import CoreUpdates from './CoreUpdates.vue';
import { useUpdates } from '../composables/useUpdates';
const core = ref<InstanceType<typeof CoreUpdates>>();
const {
  channel,
  channelPassword,
  channelChanged,
  saveChannel,
  returnStable,
  check,
  github,
  stamp,
  run,
  install,
  githubPassword,
  pending,
  busy,
  latest,
  githubProgress,
  upload,
  uploadFile,
  uploadPassword,
  uploadProgress,
  refresh,
} = useUpdates(core);
defineProps<{ tab: string }>();
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
        <div class="settings-grid">
          <label>
            Канал обновлений панели
            <select
              aria-label="Канал обновлений панели"
              v-model="channel"
              @change="channelChanged = true"
            >
              <option value="stable">Стабильный</option>
              <option value="preview">Стабильный + тестовый</option>
            </select>
          </label>
          <label>
            Пароль для смены канала
            <input
              id="github-channel-password"
              v-model="channelPassword"
              type="password"
              autocomplete="current-password"
            />
          </label>
          <ActionButton :action="saveChannel">Сохранить канал</ActionButton>
        </div>
        <p class="tiny muted">
          Тестовые сборки устанавливаются только вручную. Xray обновляется независимо.
        </p>
        <ActionButton id="github-check" :action="check">Проверить обновления</ActionButton>
        <Facts
          id="github-status"
          :rows="[
            ['Установлена', github.current],
            [
              'Канал установленной сборки',
              github.installed_channel === 'preview' ? 'Тестовый' : 'Стабильный',
            ],
            ['Канал поиска', github.channel === 'preview' ? 'Стабильный + тестовый' : 'Стабильный'],
            ['Тип найденного выпуска', github.prerelease ? 'Тестовый' : 'Стабильный'],
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
                    : github.rollback_available
                      ? 'Доступен возврат на стабильную версию'
                      : github.available
                        ? 'Доступно обновление'
                        : 'Обновлений нет'),
            ],
          ]"
        />
        <pre id="github-notes" class="log-box">{{ github.notes || '' }}</pre>
        <form
          v-show="latest || github.rollback_available"
          id="github-install-form"
          @submit.prevent="run(install)"
        >
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
          <button v-if="latest" class="primary" type="submit" :disabled="pending || busy">
            Установить проверенный выпуск
          </button>
          <ActionButton
            v-if="github.rollback_available"
            kind="danger"
            :disabled="pending || busy"
            :action="returnStable"
          >
            Вернуться на стабильный выпуск
          </ActionButton>
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
