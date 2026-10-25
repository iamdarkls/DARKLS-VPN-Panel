export const views = {
  sites: { title: 'Сайты и сервисы', subtitle: 'Выбирай, что открывать через твой VPN.' },
  devices: {
    title: 'Устройства',
    subtitle: 'Определи, какие устройства используют выбранные маршруты.',
  },
  servers: {
    title: 'VPN-серверы',
    subtitle: 'Подключения, сохранённые серверы и безопасное переключение.',
  },
  diagnostics: { title: 'Диагностика', subtitle: 'Соединение, DNS и маршрутизация сайтов.' },
  stability: {
    title: 'Контроль связи',
    subtitle: 'История доступности, восстановление и автоматический контроль.',
  },
  tuning: { title: 'Настройка связи', subtitle: 'QUIC, параметры XHTTP и задержка под нагрузкой.' },
  updates: {
    title: 'Обновления',
    subtitle: 'Панель с GitHub, ядро Xray и ручная установка — в одном месте.',
  },
  backups: {
    title: 'Копии и файлы',
    subtitle: 'Восстановление настроек, импорт, экспорт и очистка места.',
  },
  metrics: { title: 'Ресурсы роутера', subtitle: 'Фактическая память, CPU и свободное место.' },
  logs: { title: 'Журнал событий', subtitle: 'Что менялось и почему восстанавливался VPN.' },
  settings: { title: 'Пароль панели', subtitle: 'Управление доступом к локальной панели.' },
} as const;
export type View = keyof typeof views;
export const menus: [string, View[]][] = [
  ['Маршруты', ['sites', 'devices', 'servers']],
  ['Связь', ['diagnostics', 'stability', 'tuning']],
  ['Система', ['updates', 'backups', 'metrics', 'logs', 'settings']],
];
export const tabs: Partial<Record<View, readonly (readonly [string, string])[]>> = {
  servers: [
    ['main', 'Мои серверы'],
    ['backup', 'Резервный канал'],
  ],
  stability: [
    ['history', 'История связи'],
    ['recovery', 'Восстановление'],
    ['monitor', 'Автоконтроль'],
  ],
  updates: [
    ['panel', 'Панель · GitHub'],
    ['core', 'Ядро Xray'],
    ['manual', 'Установка из ZIP'],
  ],
  backups: [
    ['history', 'Копии настроек'],
    ['transfer', 'Импорт и экспорт'],
    ['files', 'Файлы и очистка'],
  ],
};
