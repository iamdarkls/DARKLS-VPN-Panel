import { computed } from 'vue';
import { draft, devices, busy, loadDevices } from '../store';
import type { Device } from '../types';

/** Combine discovered devices with saved policy entries, including offline devices.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useDevices() {
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
  return { draft, busy, loadDevices, all, selected, toggle };
}
