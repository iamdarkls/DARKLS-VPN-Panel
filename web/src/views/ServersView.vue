<script setup lang="ts">
import { switchReasonLabel, switchResultLabel } from '../labels';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import Modal from '../components/Modal.vue';
import ActionButton from '../components/ActionButton.vue';

import { useServers } from '../composables/useServers';
const {
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
} = useServers();
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
        <ActionButton :action="snapshot">Сохранить текущее в список</ActionButton>
        <ActionButton :disabled="!data.rollback_available" :action="rollback">
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
    <PanelBox
      v-show="tab === 'main'"
      title="История переключений"
      description="Последние 100 попыток за 30 дней. Идентификаторы каналов не содержат адресов и VPN-ключей."
    >
      <p>Текущий канал: {{ data.current_id || 'Не определён' }}</p>
      <div
        v-for="(entry, index) in [...(data.history || [])].reverse()"
        :key="entry.at + ':' + index"
        class="backup-row"
      >
        <div>
          <strong>{{ stamp(entry.at) }} · {{ entry.from }} → {{ entry.to }}</strong>
          <p>{{ switchReasonLabel(entry.reason) }} · {{ switchResultLabel(entry.result) }}</p>
        </div>
      </div>
      <p v-if="!data.history?.length" class="muted">Переключений после обновления пока нет.</p>
      <ActionButton :disabled="!data.rollback_available" :action="rollback">
        Проверить и вернуть предыдущий канал
      </ActionButton>
    </PanelBox>
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
          <ActionButton :disabled="pending" :action="testConnection">
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
