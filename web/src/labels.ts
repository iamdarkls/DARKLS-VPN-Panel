const applyPhases: Record<string, string> = {
  validation: 'Проверяем настройки',
  backup: 'Создаём резервную копию',
  saving: 'Сохраняем файлы',
  routing: 'Обновляем DNS и правила',
  verification: 'Проверяем сохранённые файлы',
  rollback: 'Восстанавливаем прежнее состояние',
  succeeded: 'Настройки применены',
  rolled_back: 'Прежние настройки восстановлены',
  error: 'Применение не завершено',
  unknown: 'Результат не подтверждён',
};
const applySteps: Record<string, string> = {
  validation: 'Проверка',
  backup: 'Резервная копия',
  saving: 'Сохранение',
  routing: 'DNS и правила',
  verification: 'Проверка файлов',
  rollback: 'Откат',
};
const stepStates: Record<string, string> = {
  skipped: 'Пропущено: VPN выключен',
  pending: 'Ожидает',
  running: 'Выполняется',
  done: 'Готово',
  error: 'Ошибка',
};
const switchReasons: Record<string, string> = {
  automatic: 'Автопереключение',
  profile: 'Выбор профиля',
  'connection-use': 'Подключение из списка',
  'connection-apply': 'Ручная настройка',
  'connection-rollback': 'Возврат предыдущего',
};
const switchResults: Record<string, string> = {
  confirmed: 'HTTPS подтверждён',
  configured_off: 'Сохранено при выключенном VPN',
  not_applied: 'Не применено; прежний канал сохранён',
  recovery_pending: 'Нужно завершить восстановление',
};
export const applyPhaseLabel = (key: string) => applyPhases[key] || key;
export const applyStepLabel = (key: string) => applySteps[key] || key;
export const applyStateLabel = (key: string) => stepStates[key] || key;
export const switchReasonLabel = (key: string) => switchReasons[key] || 'Ручное переключение';
export const switchResultLabel = (key: string) => switchResults[key] || key;
