<script setup lang="ts">
import PanelBox from '../components/PanelBox.vue';
import DataTable from '../components/DataTable.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';

import { useTuning } from '../composables/useTuning';
const {
  measureLoad,
  compare,
  observe,
  group,
  services,
  quic,
  temporaryGroup,
  benchmark,
  rows,
  preset,
  password,
  apply,
  via,
  load,
} = useTuning();
defineExpose({ refresh: async () => {} });
</script>
<template>
  <section id="tuning-view">
    <p class="section-intro">
      Начни с пробного режима QUIC, затем сравни параметры канала. Измерения запускаются только по
      кнопке.
    </p>
    <PanelBox
      title="Сравнить QUIC и TCP для группы"
      description="HTTPS-тесты панели используют TCP. Для сравнения QUIC включи пробный режим, перезапусти приложение на устройстве и открой тот же сервис. Таймер вернёт прежний выбор."
    >
      <label>
        Группа
        <select v-model="group">
          <option v-for="[id, label] in services" :key="id" :value="id">{{ label }}</option>
        </select>
      </label>
      <label>
        Пробный режим
        <select v-model="quic">
          <option value="true">Запретить QUIC, использовать TCP</option>
          <option value="false">Разрешить QUIC</option>
        </select>
      </label>
      <ActionButton :action="() => temporaryGroup(group, 5, quic === 'true')">
        Включить пробный режим на 5 минут
      </ActionButton>
      <ActionButton
        v-for="[verdict, label] in [
          ['good', 'Работает нормально'],
          ['slow', 'Медленно'],
          ['failed', 'Не работает'],
        ]"
        :key="verdict"
        :action="() => observe(verdict!)"
      >
        {{ label }}
      </ActionButton>
    </PanelBox>
    <PanelBox
      title="Сравнить параметры XHTTP"
      description="Текущие параметры, 2 и 4 соединения проверяются последовательно отдельным Xray. До 3 МиБ загрузок плюс HTTPS; нужен запас 96 МиБ RAM. Рабочий канал не меняется. Короткий тест не подтверждает длительную стабильность."
    >
      <ActionButton :action="compare">Сравнить три варианта</ActionButton>
      <template v-if="benchmark">
        <DataTable
          :rows="rows"
          :columns="[
            ['name', 'Вариант'],
            ['median', 'Медиана HTTPS'],
            ['fail', 'Ошибки / пробы'],
            ['speed', 'Загрузка 1 МиБ'],
            ['error', 'Результат'],
          ]"
        />
        <p class="tiny muted">{{ benchmark.note }}</p>
      </template>
      <label>
        Применить после сравнения
        <select v-model="preset">
          <option value="reuse2">2 соединения</option>
          <option value="reuse4">4 соединения</option>
          <option value="restore">Вернуть исходный XMUX этого канала</option>
        </select>
      </label>
      <label>
        Текущий пароль
        <input v-model="password" type="password" autocomplete="current-password" />
      </label>
      <ActionButton :action="apply">Проверить и применить XHTTP</ActionButton>
    </PanelBox>
    <PanelBox
      title="Задержка под нагрузкой"
      description="Ограниченная загрузка до 8 МиБ и 8 секунд. Сравни прямой канал и VPN. Роутер не включает QoS автоматически."
    >
      <label>
        Канал
        <select v-model="via">
          <option value="direct">Прямой канал</option>
          <option value="vpn">Через VPN</option>
        </select>
      </label>
      <ActionButton :action="measureLoad">Измерить задержку под нагрузкой</ActionButton>
      <template v-if="load">
        <Facts
          :rows="[
            ['Без нагрузки', load.idle_ms ? load.idle_ms.toFixed(0) + ' мс' : 'Нет успешных проб'],
            ['Под нагрузкой', load.load_ms ? load.load_ms.toFixed(0) + ' мс' : 'Нет успешных проб'],
            ['Ошибки HTTPS', load.failures],
            ['Загружено', (load.bytes / 1048576).toFixed(2) + ' МиБ'],
            ['Выборка достаточна', load.enough_samples ? 'Да' : 'Нет'],
          ]"
        />
        <p>{{ load.note }}</p>
      </template>
    </PanelBox>
  </section>
</template>
