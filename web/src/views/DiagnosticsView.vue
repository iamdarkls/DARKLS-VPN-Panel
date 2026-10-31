<script setup lang="ts">
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import DataTable from '../components/DataTable.vue';
import ActionButton from '../components/ActionButton.vue';
import { useDiagnostics } from '../composables/useDiagnostics';
const {
  checkDependencies,
  checkConnectivity,
  probeTunnel,
  probeService,
  service,
  services,
  serviceResult,
  probeFacts,
  device,
  devices,
  domain,
  diagnose,
  diagnosis,
  connectivity,
  rows,
  dependencyService,
  dependencies,
  dependencyRows,
  loadDevices,
} = useDiagnostics();
defineExpose({ refresh: loadDevices });
</script>
<template>
  <section id="diagnostics-view">
    <PanelBox
      title="Ответ сервиса через VPN"
      description="Проверка с роутера через SOCKS Xray: DNS и HTTPS. Не проверяет вход в аккаунт, QUIC или маршрут браузера. Один сбой не перезапускает VPN."
    >
      <form id="service-probe-form" class="inline-form" @submit.prevent>
        <select id="service-probe-select" v-model="service" aria-label="Сервис">
          <option v-for="[id, name] in services" :key="id" :value="id">{{ name }}</option>
        </select>
        <ActionButton kind="primary" :action="probeService">Проверить HTTPS</ActionButton>
      </form>
      <Facts
        v-if="serviceResult"
        id="service-probe-result"
        :rows="[
          ['Сервис', serviceResult.domain],
          ['Результат', serviceResult.message],
          ['HTTP', serviceResult.http || '—'],
          ['Время', serviceResult.duration_ms + ' мс'],
        ]"
      />
    </PanelBox>
    <PanelBox title="Соединение через VPN">
      <ActionButton id="probe-now" :action="probeTunnel">Проверить сейчас</ActionButton>
      <Facts id="probe-details" :rows="probeFacts" />
    </PanelBox>
    <PanelBox
      title="Проверить маршрут устройства"
      description="Прогноз TCP-маршрута по DNS роутера, IP-наборам и режиму устройств. Реальный запрос с устройства не отправляется."
    >
      <form id="diagnose-form" class="inline-form" @submit.prevent>
        <select id="diagnose-device" v-model="device" aria-label="Устройство для диагностики">
          <option value="">Это устройство</option>
          <option v-for="d in devices.filter((x) => x.ip)" :key="d.mac" :value="d.mac">
            {{ d.name }} · {{ d.ip }}
          </option>
        </select>
        <input
          id="diagnose-domain"
          v-model="domain"
          placeholder="example.com или https-ссылка"
          aria-label="Домен для проверки"
          required
        />
        <ActionButton kind="primary" :action="diagnose">Проверить сайт</ActionButton>
      </form>
      <div id="diagnose-result" class="result-box">
        <template v-if="diagnosis">
          <Facts
            :rows="[
              ['Домен', diagnosis.domain],
              ['Устройство', (diagnosis.device?.name || '') + ' · ' + diagnosis.client_ip],
              [
                'Выбор и IP-набор',
                diagnosis.policy_mismatch
                  ? 'Расходятся: дождитесь синхронизации устройств'
                  : 'Согласованы',
              ],
              ['Разрешено режимом устройств', diagnosis.device_eligible ? 'Да' : 'Нет'],
              ['Правила роутера', diagnosis.routes_active ? 'Подключены' : 'Не подключены'],
              [
                'Группы',
                diagnosis.groups
                  .map((g) => g.name + (g.enabled ? ' · вкл.' : ' · выкл.'))
                  .join(', ') || 'Совпадений нет',
              ],
            ]"
          />
          <p v-for="a in diagnosis.addresses" :key="a.ip" class="route-result">
            {{ a.ip }} →
            {{
              a.via_vpn
                ? 'По правилам должен идти через VPN'
                : 'По проверенным правилам VPN не выбран'
            }}{{ a.reason ? ' · ' + a.reason : '' }}
          </p>
        </template>
      </div>
    </PanelBox>
    <PanelBox
      title="Что именно не отвечает"
      description="Прямой HTTPS, TCP VPN-сервера, DNS, правила и общий туннель проверяются отдельно. Проверка ограничена 45 секундами."
    >
      <ActionButton :action="checkConnectivity">Проверить связь</ActionButton>
      <Facts :rows="rows" />
      <p v-for="(value, key) in connectivity?.errors" :key="key" class="tiny muted">
        {{ key }}: {{ value }}
      </p>
      <a class="secondary download" href="/api/report">Скачать отчёт о сбое ↓</a>
      <p class="tiny muted">
        JSON: версии, память, правила и последние ошибки. Пароли и VPN-ключи исключены, IP и MAC
        заменены метками. Сбор — до 60 секунд.
      </p>
    </PanelBox>
    <PanelBox
      title="Зависимости сервиса"
      description="DNS, включённая группа и IP в VPN-наборе. HTTPS основной страницы проверяется отдельно; учётная запись и браузер не проверяются."
    >
      <label>
        Сервис
        <select v-model="dependencyService">
          <option v-for="[id, name] in services" :key="id" :value="id">{{ name }}</option>
        </select>
      </label>
      <ActionButton :action="checkDependencies">Проверить зависимости</ActionButton>
      <template v-if="dependencies">
        <DataTable
          :rows="dependencyRows"
          :columns="[
            ['domain', 'Домен'],
            ['dns', 'DNS'],
            ['route', 'Настройка маршрута'],
            ['set', 'IP в VPN-наборе'],
            ['https', 'HTTPS через VPN'],
            ['ip', 'IPv4'],
          ]"
        />
        <p>{{ dependencies.https.message }}</p>
        <p class="tiny muted">{{ dependencies.note }}</p>
      </template>
    </PanelBox>
  </section>
</template>
