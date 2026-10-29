const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 let reads=0;await page.route('**/api/logs',async r=>{reads++;await r.fulfill({json:{events:[{at:1792954914,message:'VPN HTTPS failed: timeout'},{at:1792954929,message:'HTTPS восстановлен'}],watch:'INFO monitor ready\nWARNING sample warning\nERROR sample failure',panel:'INFO panel ready',update:'INFO integrity OK'}})});
 await page.goto('http://127.0.0.1:18088');await page.locator('#login-form input[name=password]').fill('preview-password');await page.locator('#login-form button').click();await page.locator('.card').first().waitFor();
 await page.locator('#theme-select').selectOption('dark');
 await page.getByLabel('Свернуть или развернуть меню').click();assert.equal(await page.locator('#app').evaluate(n=>n.classList.contains('sidebar-collapsed')),true);
 await page.reload();await page.locator('.card').first().waitFor();assert.equal(await page.locator('#app').evaluate(n=>n.classList.contains('sidebar-collapsed')),true);
 await page.getByLabel('Свернуть или развернуть меню').click();await page.getByLabel('Поиск раздела').fill('сервер');assert.equal(await page.locator('.nav').count(),1);await page.getByLabel('Поиск раздела').fill('');
 await page.locator('[data-view=metrics]').click();await page.locator('.metric-card').first().waitFor();await page.screenshot({path:'/tmp/darkls-design-desktop.png',fullPage:true});
 assert.equal(reads,0);await page.locator('#open-logs').click();const modal=page.getByRole('dialog',{name:'Журналы',exact:true});await modal.waitFor();await page.locator('.log-line').first().waitFor();assert.equal(reads,1);
 await page.getByLabel('Источник журнала').selectOption('watch');assert.equal(await page.locator('.log-line').count(),3);await page.getByLabel('Уровень записей').selectOption('error');assert.equal(await page.locator('.log-line').count(),1);
 await page.getByLabel('Поиск в журнале').fill('no match');assert.equal(await page.locator('.log-line').count(),0);await page.getByLabel('Поиск в журнале').fill('');
 const download=page.waitForEvent('download');await modal.getByRole('button',{name:'Скачать',exact:true}).click();assert.equal((await download).suggestedFilename(),'darkls-watch.log');
 await page.clock.install();await page.getByRole('checkbox',{name:'Автообновление',exact:true}).check();await Promise.all([page.waitForResponse(r=>r.url().endsWith('/api/logs')),page.clock.fastForward(5100)]);assert.equal(reads,2);
 await page.screenshot({path:'/tmp/darkls-design-logs.png'});await page.keyboard.press('Escape');await modal.waitFor({state:'detached'});await page.clock.fastForward(10000);assert.equal(reads,2);
 for(const theme of ['light','dark']) {await page.locator('#theme-select').selectOption(theme);await page.setViewportSize({width:390,height:844});await page.locator('#open-logs').click();await modal.waitFor();assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.equal(await modal.evaluate(n=>n.scrollWidth>n.clientWidth),false);await page.screenshot({path:'/tmp/darkls-design-mobile-'+theme+'.png'});await page.getByRole('button',{name:'Закрыть журналы',exact:true}).click();}
 assert.deepEqual(errors,[]);await browser.close();console.log('PASS: sidebar persistence/search, dark/light mobile, logs modal, filtering, download, opt-in polling and cleanup');
})().catch(e=>{console.error(e);process.exit(1)});
