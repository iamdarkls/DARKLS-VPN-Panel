const { chromium } = require('playwright');
const assert = require('node:assert/strict');

(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    page.on('dialog', (dialog) => dialog.accept());
    await page.goto('http://127.0.0.1:18088');
    await page.locator('#login-form input[name=password]').fill('preview-password');
    await page.locator('#login-form button').click();
    await page.locator('[data-view=servers]').click();

    const box = page.locator('.panel-box').filter({
      has: page.getByRole('heading', { name: 'VPN-подписка', exact: true }),
    });
    const source = box.getByLabel('HTTPS-ссылка');
    const password = box.getByLabel('Пароль панели');
    const save = box.getByRole('button', { name: 'Сохранить и привязать' });
    const auto = box.getByRole('checkbox');
    assert.equal(await auto.isChecked(), false);
    const secret = 'test-subscription-token';
    await source.fill('https://example.com:2096/sub/' + secret);
    await box.getByLabel('Точное имя профиля').fill('Main NL');
    await password.fill('preview-password');
    await save.click();
    await box.getByText(/Привязана к текущему профилю/).waitFor();
    assert.equal(await source.inputValue(), '');
    assert.equal(await password.inputValue(), '');
    assert.equal(await auto.isChecked(), false);

    const refreshed = page.waitForResponse((r) => r.url().endsWith('/api/connections'));
    await auto.check();
    await password.fill('preview-password');
    await save.click();
    const response = await refreshed;
    const settings = (await response.json()).subscription;
    assert.equal(settings.auto, true);
    assert.equal(settings.profile, 'Main NL');
    assert.ok(!JSON.stringify(settings).includes(secret));
    await box.getByText(/Автообновление включено/).waitFor();

    await password.fill('preview-password');
    await box.getByRole('button', { name: 'Удалить подписку' }).click();
    await box.getByText(/Привязана к текущему профилю/).waitFor({ state: 'hidden' });
    assert.equal(await box.getByRole('button', { name: 'Проверить и применить' }).isDisabled(), true);
    console.log('PASS: subscription save, opt-in, secret handling and removal through real API');
  } finally {
    await browser.close();
  }
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
