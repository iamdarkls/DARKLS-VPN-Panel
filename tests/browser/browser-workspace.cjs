const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 const page=await browser.newPage({viewport:{width:1400,height:1100}}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 let accept=true,dialogs=0;
 page.on('dialog',async d=>{dialogs++;if(accept)await d.accept();else await d.dismiss();});
 await page.route('**/api/status',r=>r.fulfill({json:{active:true,xray:true,dns:true,nat:true,udp:true,watch:true}}));
 await page.route('**/api/activity',r=>r.fulfill({json:{summary:{},notifications:[]}}));
 await page.goto('http://127.0.0.1:18088');await page.locator('#login-form input[name=password]').fill('preview-password');await page.locator('#login-form button').click();await page.locator('.card').first().waitFor();
 // Cancelled closes must retain forms; confirmed closes clear both visible and secret fields.
 await page.locator('[data-view=servers]').click();await page.getByRole('button',{name:'+ Добавить сервер',exact:true}).click();
 const editor=page.locator('.connection-dialog');await editor.getByLabel('Название сервера',{exact:true}).fill('Unsaved');await editor.getByLabel('VLESS-ссылка или outbound JSON',{exact:true}).fill('vless://private-token@private.example:443');await editor.getByLabel('Пароль для управления подключениями').fill('private-password');
 accept=false;await page.keyboard.press('Escape');assert.equal(await editor.isVisible(),true);assert.equal(await editor.getByLabel('Название сервера',{exact:true}).inputValue(),'Unsaved');
 accept=true;await page.keyboard.press('Escape');await editor.waitFor({state:'hidden'});assert.equal(await editor.getByLabel('Пароль для управления подключениями').inputValue(),'');
 await page.locator('[data-view=sites]').click();await page.locator('#add-group').click();await page.locator('#group-name').fill('Do not lose');accept=false;await page.keyboard.press('Escape');assert.equal(await page.locator('#group-name').inputValue(),'Do not lose');accept=true;await page.keyboard.press('Escape');await page.locator('#edit-form').waitFor({state:'hidden'});
 // A stale progress response from another operation must not replace the active operation.
 let operation,phase='routing',releaseApply;
 const applyGate=new Promise(resolve=>releaseApply=resolve);let polls=0;
 await page.route('**/api/apply',async r=>{operation=r.request().postDataJSON().operation_id;await applyGate;await r.fulfill({json:{ok:true}});});
 await page.route('**/api/apply-status',r=>{polls++;return r.fulfill({json:polls===1?{id:'other-operation',phase:'error',message:'FOREIGN RESULT'}:{id:operation,phase,finished:phase==='succeeded'?1793390000:undefined,saved:true,verified:phase==='succeeded',message:phase==='succeeded'?'Файлы проверены; связь проверяется отдельно.':'DNS и правила обновляются',steps:[{name:'routing',state:phase==='succeeded'?'done':'running'}]}});});
 await page.locator('.card').first().locator('.switch').click();await page.locator('#apply').click();await page.getByRole('button',{name:'Применить эти изменения',exact:true}).click();
 const progress=page.getByRole('dialog',{name:'Применение настроек',exact:true});await progress.waitFor();await progress.getByText('DNS и правила обновляются',{exact:true}).waitFor();assert.equal(await page.getByText('FOREIGN RESULT',{exact:true}).count(),0);
 phase='succeeded';releaseApply();await progress.getByText('Файлы проверены; связь проверяется отдельно.',{exact:true}).waitFor();await progress.getByRole('button',{name:'Закрыть результат'}).click();await progress.waitFor({state:'hidden'});
 // History contains outcomes, including disabled configuration, without exposing outbound data.
 await page.route('**/api/connections',r=>r.fulfill({json:{items:[],current:{},current_id:'server-abc',history:[{at:1793390000,from:'server-old',to:'server-abc',reason:'automatic',result:'configured_off',verified:false}]}}));await page.locator('[data-view=servers]').click();await page.getByText('Сохранено при выключенном VPN',{exact:false}).waitFor();
 // Channel persistence is explicit and does not trigger installation.
 let selected='stable',channelBody,installBody;
 await page.route('**/api/github',r=>r.fulfill({json:{configured:true,current:'9.1.0',channel:selected,installed_channel:'preview',latest:'9.0.0',signed:true,rollback_available:selected==='stable'}}));
 await page.route('**/api/github-channel',r=>{channelBody=r.request().postDataJSON();selected=channelBody.channel;return r.fulfill({json:{ok:true}});});
 await page.locator('[data-view=updates]').click();const channel=page.getByLabel('Канал обновлений панели',{exact:true});await channel.selectOption('preview');await page.getByLabel('Пароль для смены канала').fill('preview-password');await page.getByRole('button',{name:'Сохранить канал',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#github-channel-password').value==='');assert.equal(channelBody.channel,'preview');assert.equal(await page.getByLabel('Пароль для смены канала').inputValue(),'');assert.equal(installBody,undefined);
 await channel.selectOption('stable');await page.getByLabel('Пароль для смены канала').fill('preview-password');await page.getByRole('button',{name:'Сохранить канал',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#github-channel-password').value==='');await page.getByRole('button',{name:'Вернуться на стабильный выпуск',exact:true}).waitFor();
 accept=false;await page.locator('#github-password').fill('preview-password');await page.getByRole('button',{name:'Вернуться на стабильный выпуск',exact:true}).click();assert.equal(installBody,undefined);assert.equal(await page.locator('#github-password').inputValue(),'');
 accept=true;await page.route('**/api/github-install',r=>{installBody=r.request().postDataJSON();return r.fulfill({status:202,json:{job:{id:'return-stable',phase:'staged'}}});});await page.route('**/api/update-status',r=>r.fulfill({json:{job:{id:'return-stable',phase:'rolled_back',message:'Проверка завершена без установки'}}}));await page.locator('#github-password').fill('preview-password');await page.getByRole('button',{name:'Вернуться на стабильный выпуск',exact:true}).click();await page.locator('.toast-body').filter({hasText:'Проверка завершена без установки'}).waitFor();assert.equal(installBody.rollback,true);assert.equal(installBody.version,'9.0.0');assert.equal(await page.locator('#github-password').inputValue(),'');
 await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.ok(dialogs>=5);assert.equal(await page.evaluate(()=>Object.values(localStorage).some(v=>v.includes('private-token')||v.includes('private-password'))),false);assert.deepEqual(errors,[]);
 await browser.close();console.log('PASS: unsaved-form cancel/confirm, secret disposal, correlated live apply progress, switch outcomes, channel persistence and explicit stable return');
})().catch(e=>{console.error(e);process.exit(1)});
