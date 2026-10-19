# Документация DARKLS VPN Panel

Актуальное руководство находится в [README.md](../README.md).

- [Устройство проекта и восстановление](ARCHITECTURE.md)
- [Источники каталога сервисов](catalog/SERVICES.md)
- [Источники каталога 18+](catalog/ADULT.md)

Старые инструкции по ручной установке промежуточных версий заменены актуальным порядком обновления через панель. Существующие настройки и пароль сохраняются.

## Команды администратора

Выполняются в **SSH роутера**, не в PowerShell Windows:

```sh
/jffs/xray/panel/darkls-panel --version
/jffs/xray/xray version
/jffs/xray/panel/darkls-panel --check-router
/jffs/xray/panel/darkls-panel --check-vpn
/jffs/xray/panel/darkls-panel --check-components
```

`--recover-components` восстанавливает неисправные компоненты. `--rollback-xray` возвращает проверенную предыдущую копию ядра с USB; соединения переподключатся. Для обычного управления используйте веб-панель.

Не публикуйте `config-vpn.json`, содержимое `profiles/`, `auth.json` или VPN-ссылки. Для обращения за помощью используйте отчёт из панели и проверьте его перед отправкой.
