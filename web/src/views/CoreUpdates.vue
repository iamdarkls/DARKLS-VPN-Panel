<script setup lang="ts">
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';

import { useCoreUpdates } from '../composables/useCoreUpdates';
const {
  check,
  facts,
  load,
  data,
  stamp,
  changed,
  form,
  password,
  retry,
  save,
  latest,
  working,
  install,
  rollback,
} = useCoreUpdates();
defineExpose({ refresh: load });
</script>
<template>
  <PanelBox
    title="Обновления ядра Xray"
    class="wide"
    description="Официальные стабильные выпуски и пререлизы XTLS/Xray-core для Linux ARM64. Автоустановка изначально выключена. При замене ядра VPN-соединения кратко переподключатся."
  >
    <Facts :rows="facts" />
    <ActionButton :action="load">Обновить сведения Xray</ActionButton>
    <pre class="log-box">{{ data.status.notes || '' }}</pre>
    <p class="tiny muted" role="status">
      {{
        data.job.message
          ? stamp(data.job.started) + ' · ' + data.job.message
          : 'Операций обновления ядра ещё не было.'
      }}
    </p>
    <ActionButton :action="check">Проверить обновления Xray</ActionButton>
    <div @input="changed = true" @change="changed = true">
      <label class="quic-label">
        <input v-model="form.check" type="checkbox" />
        Автоматически проверять обновления Xray
      </label>
      <label class="quic-label">
        <input v-model="form.auto_install" type="checkbox" />
        Устанавливать обновления, включая пререлизы, автоматически
      </label>
      <label>
        Час автоустановки, московское время (0–23)
        <input v-model.number="form.hour_msk" type="number" min="0" max="23" />
      </label>
      <label>
        Отсрочка после публикации, дней (0–30)
        <input v-model.number="form.delay_days" type="number" min="0" max="30" />
      </label>
    </div>
    <p class="tiny muted">
      Автоустановка требует USB WINDOWS_10 и свежую успешную проверку VPN. Неудачная версия
      блокируется для автоматических повторов; за ночь — не больше одной попытки. Скачанные файлы
      находятся на USB и удаляются после операции.
    </p>
    <label>
      Текущий пароль панели
      <input v-model="password" type="password" autocomplete="current-password" />
    </label>
    <label class="quic-label">
      <input v-model="retry" type="checkbox" />
      Явно повторить установку ранее неудачной версии
    </label>
    <ActionButton :action="save">Сохранить режим обновления Xray</ActionButton>
    <ActionButton kind="primary" :disabled="!latest || working" :action="install">
      Установить проверенный Xray
    </ActionButton>
    <ActionButton :disabled="!data.rollback_available || working" :action="rollback">
      Вернуть предыдущее ядро с USB
    </ActionButton>
  </PanelBox>
</template>
