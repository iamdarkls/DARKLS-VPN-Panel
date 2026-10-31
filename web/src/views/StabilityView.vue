<script setup lang="ts">
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import DataTable from '../components/DataTable.vue';
import ActionButton from '../components/ActionButton.vue';

import { useStability } from '../composables/useStability';
const {
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
} = useStability();
defineProps<{ tab: string }>();
defineExpose({ refresh });
</script>
<template>
  <section id="stability-view">
    <div
      id="stability-history"
      v-show="tab === 'history'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-stability-history"
      tabindex="0"
    >
      <PanelBox
        title="Сводка за последние 24 часа"
        description="Доступность по HTTPS-пробам общего туннеля. Состояние отдельных сайтов может отличаться."
      >
        <Facts :rows="summary" />
        <p class="tiny muted">{{ activity.summary?.note }}</p>
      </PanelBox>
      <PanelBox
        title="История состояния сервисов"
        description="Ошибка DNS, тайм-аут, ответ сайта и Cloudflare показаны отдельно."
      >
        <ActionButton :action="loadHistory">Обновить историю</ActionButton>
        <DataTable
          :rows="rows"
          :columns="[
            ['service', 'Сервис'],
            ['state', 'Последний результат'],
            ['since', 'Состояние с'],
            ['at', 'Проверено'],
            ['tunnel', 'Общий туннель'],
          ]"
        />
        <details>
          <summary>Последние изменения и длительность</summary>
          <p
            v-for="(r, i) in (history.events || [])
              .filter((r) => r.previous)
              .slice(-30)
              .reverse()"
            :key="i"
            class="tiny"
          >
            {{ stamp(r.at) }} · {{ r.service }}: {{ serviceLabel(r.previous || '') }} →
            {{ serviceLabel(r.state) }} · прежнее состояние {{ r.previous_duration_seconds }} с
          </p>
        </details>
        <p class="tiny muted">{{ history.retention }}</p>
      </PanelBox>
      <PanelBox
        title="Уведомления о связи"
        description="События за сутки. Отметка прочитанного хранится только в этом браузере."
      >
        <p v-for="item in activity.notifications" :key="item.id">
          {{ stamp(item.at) }} · {{ item.message }}
        </p>
        <p v-if="!activity.notifications?.length" class="muted">Событий за сутки нет.</p>
      </PanelBox>
    </div>
    <div
      id="stability-recovery"
      v-show="tab === 'recovery'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-stability-recovery"
      tabindex="0"
    >
      <PanelBox
        title="Завершить незавершённую операцию"
        description="Подключи USB с резервной копией и повтори откат. Изменения доступны после успешного восстановления."
      >
        <label>
          Пароль для завершения восстановления
          <input v-model="password" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :disabled="recoveryBusy" :action="() => recoverPending()">
          Повторить восстановление
        </ActionButton>
        <template v-if="status.offline_recovery_available">
          <p class="muted">
            Если прежний сервер недоступен, сохраните копию и завершите откат с выключенным VPN.
            После этого можно выбрать новое подключение. Эта операция не подтверждает
            работоспособность туннеля и не включает VPN автоматически.
          </p>
          <ActionButton :disabled="recoveryBusy" :action="() => recoverPending(true)">
            Восстановить с выключенным VPN
          </ActionButton>
        </template>
      </PanelBox>
      <PanelBox
        title="Восстановить отдельную часть"
        description="Автоконтроль сначала чинит процессы, DNS helper и правила. Xray перезапускается при подтверждённой неисправности туннеля или DNS-входа."
      >
        <ActionButton
          v-for="[component, label] in [
            ['auto', 'Найти и восстановить'],
            ['dns', 'Только DNS helper'],
            ['rules', 'Только правила'],
          ]"
          :key="component"
          :action="() => repair(component!)"
        >
          {{ label }}
        </ActionButton>
        <label>
          Пароль для ручного перезапуска Xray
          <input v-model="xrayPassword" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="() => repair('xray')">Перезапустить Xray</ActionButton>
      </PanelBox>
    </div>
    <div
      id="stability-monitor"
      v-show="tab === 'monitor'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-stability-monitor"
      tabindex="0"
    >
      <PanelBox
        title="Контроль связи"
        class="wide"
        description="Проверки работают независимо от открытой панели. Отказ одного сервиса не перезапускает Xray. Резерв включается при подтверждённом отказе туннеля; автоматического возврата на основной нет."
      >
        <div @input="changed = true" @change="changed = true">
          <label class="quic-label">
            <input v-model="form.monitor_services" type="checkbox" />
            Периодически проверять включённые сервисы
          </label>
          <label>
            Интервал в секундах: 120–3600
            <input v-model.number="form.interval_seconds" type="number" min="120" max="3600" />
          </label>
          <label class="quic-label">
            <input v-model="form.auto_failover" type="checkbox" :disabled="!backup" />
            Автоматически переключаться на проверенный резерв
          </label>
        </div>
        <label>
          Текущий пароль
          <input v-model="monitorPassword" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="save">Сохранить контроль связи</ActionButton>
      </PanelBox>
    </div>
  </section>
</template>
