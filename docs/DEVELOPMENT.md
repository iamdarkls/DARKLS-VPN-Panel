# Разработка

## Инструменты

- Go **1.27.2**, Python 3, Node.js **24**.
- Vue 3, TypeScript и Vite закреплены точными версиями в `web/package-lock.json`.
- Для браузерных сценариев — Playwright из `tools/browser/package-lock.json` и Chromium.

Node.js нужен для разработки и сборки. Роутер запускает один Go-бинарник со встроенными сжатыми ресурсами; Vue выполняется в браузере. Внешних Go-модулей, CDN, удалённых шрифтов и серверного Node.js нет.

## Собрать интерфейс

Из корня репозитория:

```sh
npm ci --prefix web
npm run --prefix web build
```

Команда проверяет TypeScript, собирает `web/dist` через Vite и обновляет `web/assets_generated.go`. В нём находятся только gzip-представления трёх production-ресурсов и отпечатки исходников. `web/dist` и `node_modules` не коммитятся; `assets_generated.go` коммитится вместе с исходниками.

После редактирования компонентов:

```sh
npm run --prefix web format
npm run --prefix web build
gofmt -w web/assets_generated.go
```

Отдельные команды: `npm run --prefix web typecheck` и `npm run --prefix web format:check`.

## Локальный интерфейс и тестовая панель

```sh
DARKLS_BROWSER_PREVIEW=1 go test -run '^TestBrowserHarness$' -v ./internal/panel
```

Тестовая панель доступна на `http://127.0.0.1:18088`, пароль `preview-password`; данные временные, реальный VPN не используется. Для HMR в другом терминале запусти `npm run --prefix web dev`: локальный Vite проксирует `/api` на эту тестовую панель. Эта настройка не попадает в production-пакет.

`npm run --prefix web preview` показывает статическую production-сборку; для полноценной работы с авторизацией нужен Go-сервер и его API.

## Проверки

Сначала собери интерфейс, затем:

```sh
python3 tools/build_assets.py --check
go test -race ./...
go vet ./...
python3 -m unittest discover -s tools -p 'test_*.py'
```

Go-тесты проверяют раздачу именно `web/dist`: gzip/identity, HEAD, ETag, отсутствие кеширования API и точный список публичных ресурсов. Сервер не раздаёт `.vue`, TypeScript, исходный HTML или произвольные пути под `/assets`.

Для браузерных проверок:

```sh
npm ci --prefix tools/browser
tools/browser/node_modules/.bin/playwright install --with-deps chromium
NODE_PATH="$PWD/tools/browser/node_modules" python3 tests/integration/run_browser.py
```

`CHROME_PATH` задаёт другой Chromium; `GO_BIN` и `NODE_BIN` — пути к инструментам. Можно передать имена отдельных сценариев после `run_browser.py`. Тесты работают через пользовательский интерфейс и HTTP, не через глобальные функции старого приложения.

Проверяются каталог, группы и предпросмотр, выбор устройств, черновики, редактор XHTTP/gRPC с сохранением незнакомых полей, очищение секретов, обновления и откат, темы, уведомления поверх диалогов, мобильная вёрстка и завершение сеанса во время запросов.

## Установка и сеть

Установочные сценарии принимают пути к двум исполняемым бинарникам для архитектуры тестовой машины:

```sh
export DARKLS_BASELINE_BINARY=/tmp/darkls-baseline-bin
export DARKLS_CURRENT_BINARY=/tmp/darkls-current-bin
sudo --preserve-env=DARKLS_BASELINE_BINARY,DARKLS_CURRENT_BINARY python3 tests/integration/test_installer.py
sudo --preserve-env=DARKLS_BASELINE_BINARY,DARKLS_CURRENT_BINARY python3 tests/integration/test_ota.py
sudo --preserve-env=DARKLS_BASELINE_BINARY,DARKLS_CURRENT_BINARY python3 tests/integration/test_recovery_mode.py
python3 tests/integration/test_supervisor.py
```

Файлы и команды роутера заменены временными фикстурами. Сценарии проверяют пароль, выбор сайтов, состав пакета, запуск новой версии и возврат прежней.

`sudo python3 tests/integration/test_network.py` требует Linux, iproute2, iptables и разрешение на network namespaces. Создаются изолированный bridge/router и три клиента: параллельные TCP-передачи, медленный DNS, отказ backend и возврат NAT-hook. Файрвол хоста не меняется. В CI тест обязателен; ограниченные контейнеры без CAP_NET_ADMIN не могут выполнить его локально.

Проверка XHTTP и прошивки ASUS на настоящем устройстве описана в [ROUTER_SOAK.md](ROUTER_SOAK.md).

## Пакет и выпуск

```sh
python3 tools/build_release.py
```

По умолчанию версия берётся из `release-version.txt`, архитектура — `arm64`. `--arch amd64` предназначен для тестов. `--version` меняет версию только этой сборки; `--output-dir` задаёт выходной каталог. Перед Go-сборкой проверяются отпечатки frontend-исходников. Сборщик не загружает зависимости самостоятельно: `npm ci` и `npm run build` выполняются заранее.

ZIP содержит ровно шесть файлов: бинарник, четыре shell-скрипта и `manifest.json`. `node_modules`, исходники и файлы разработки в пакет не входят. Ключ подписи — `assets/github-public-key.txt`; приватный ключ хранится только в GitHub Actions secret `RELEASE_SIGNING_KEY`.

Перед выпуском согласуй `release-version.txt`, версию в `internal/panel/cli.go`, `web/package.json` и раздел `CHANGELOG.md`. GitHub Release получает описание из CHANGELOG. README остаётся руководством.

## CI

`ci.yml` запускает reusable `validate.yml` один раз на коммит. Интерфейс проходит `npm ci`, `vue-tsc` и production-сборку до Go-тестов. При изменении версии на main reusable `release.yml` публикует только успешно проверенный коммит и повторно собирает frontend из lockfile перед подписанием пакета.

Все Actions закреплены по SHA и используют Node.js 24. npm-кеш учитывает оба lockfile, Chromium кешируется отдельно. Ограниченные повторы разрешены только загрузке зависимостей; ошибки тестов не маскируются повторами.
