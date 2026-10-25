<script setup lang="ts">
import { computed } from 'vue';
import { draft, devices, busy, loadDevices } from '../store';
import type { Device } from '../types';
import PanelBox from '../components/PanelBox.vue';
import ActionButton from '../components/ActionButton.vue';
const all = computed(() => {
  const list = [...devices.value];
  for (const d of draft.value?.policy.devices || [])
    if (!list.some((x) => x.mac === d.mac)) list.push(d);
  return list;
});
function selected(mac: string) {
  return draft.value?.policy.devices.some((d) => d.mac === mac);
}
function toggle(d: Device, checked: boolean) {
  if (!draft.value) return;
  draft.value.policy.devices = draft.value.policy.devices.filter((x) => x.mac !== d.mac);
  if (checked) draft.value.policy.devices.push({ mac: d.mac, name: d.name || '' });
}
defineExpose({ refresh: loadDevices });
</script>
<template>
  <section id="devices-view">
    <PanelBox
      title="Для каких устройств использовать VPN"
      description="Выбранные сайты будут идти через VPN только с учётом этого режима."
    >
      <label>
        Режим
        <select v-if="draft" id="device-mode" v-model="draft.policy.mode" :disabled="busy">
          <option value="all">Вся домашняя сеть</option>
          <option value="include">Только выбранные устройства</option>
          <option value="exclude">Все, кроме выбранных</option>
        </select>
      </label>
      <ActionButton id="refresh-devices" :action="loadDevices">Обновить список</ActionButton>
      <p id="device-summary" class="tiny muted">
        {{
          draft?.policy.mode === 'all'
            ? 'VPN применяется ко всей домашней сети.'
            : 'Выбрано устройств: ' + draft?.policy.devices.length
        }}
      </p>
      <div id="devices-list" class="device-list">
        <label v-for="d in all" :key="d.mac" class="device-row">
          <input
            type="checkbox"
            :aria-label="d.name || d.ip || d.mac"
            :checked="selected(d.mac)"
            :disabled="busy || draft?.policy.mode === 'all'"
            @change="toggle(d, ($event.target as HTMLInputElement).checked)"
          />
          <span>
            <strong>{{ d.name || 'Устройство' }}</strong>
            <span class="tiny muted">
              {{ d.ip || 'IP пока не найден' }} · {{ d.mac }} ·
              {{ d.online ? 'видно в ARP' : 'известно роутеру' }}
            </span>
          </span>
        </label>
        <div v-if="!all.length" class="empty">
          Пока нет устройств в DHCP/ARP. Подключи устройство к домашней сети и обнови список.
        </div>
      </div>
      <p class="footnote">
        Выбор сохраняется по MAC-адресу. IP обновляются каждые 15 секунд. Уже открытые соединения
        могут сохранять прежний маршрут. Если устройство сменит приватный MAC, выбери его заново.
      </p>
    </PanelBox>
  </section>
</template>
