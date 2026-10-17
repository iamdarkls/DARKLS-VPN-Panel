# Документация DARKLS VPN Panel

Актуальное руководство находится в [README.md](README.md).

- [Обновления и история версий](CHANGELOG.md)
- [Устройство проекта и восстановление](docs/ARCHITECTURE.md)
- [Источники каталога сервисов](CATALOG_SOURCES.md)
- [Источники каталога 18+](ADULT_DOMAINS_SOURCES.md)

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
