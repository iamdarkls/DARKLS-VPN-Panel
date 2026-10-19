const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[],reads=[];
  page.on('pageerror',e=>errors.push(e.message));
  const response=(path,data)=>page.route('**/api/'+path,r=>{reads.push(path);return r.fulfill({json:data})});
  await response('status',{active:true,disabled:false,nat:true,udp:true,xray:true,dns:true,watch:true});
  await response('github',{configured:true,current:'1.7.0',available:false});
  await response('xray',{status:{current:'26.10.10'},settings:{check:true,hour_msk:4,delay_days:3},job:{}});
  await response('preferences',{monitor_services:true,interval_seconds:300,auto_failover:false});
  await response('profiles',{active:'primary',backup_configured:false,auto_failover:false});
  await response('storage',{items:[],cleanup:{enabled:true},retention_days:7});
  await page.goto('http://127.0.0.1:18088');
  await page.locator('#login-form input[name=password]').fill('preview-password');
  await page.locator('#login-form button').click();await page.locator('.card').first().waitFor();
  // Moving a form must not duplicate IDs or drop its event handlers.
  assert.deepEqual(await page.evaluate(()=>{const ids=[...document.querySelectorAll('[id]')].map(n=>n.id);return ids.filter((v,i)=>ids.indexOf(v)!==i)}),[]);
  assert.equal(await page.locator('.nav-group').count(),3);
  await page.locator('[data-view=updates]').click();await page.locator('#github-status .fact').first().waitFor();
  assert(!reads.includes('xray'));assert(!reads.includes('preferences'));
  assert.equal(await page.locator('#updates-view #github-check').count(),1);
  assert.equal(await page.locator('#updates-view #update-form').count(),1);
  assert.equal(await page.locator('#updates-core').isVisible(),false);
  await page.locator('#tab-updates-panel').focus();await page.keyboard.press('ArrowRight');
  await page.getByText('26.10.10',{exact:true}).waitFor();
  assert.equal(await page.locator('#tab-updates-core').getAttribute('aria-selected'),'true');
  assert.equal(await page.locator('#tab-updates-core').getAttribute('tabindex'),'0');
  assert.equal(await page.locator('#github-check').isVisible(),false);
  const hour=page.locator('#updates-core').getByLabel('Час автоустановки, московское время (0–23)',{exact:true});
  await hour.fill('9');
  await page.locator('[data-view=sites]').click();await page.locator('[data-view=updates]').click();
  assert.equal(await hour.inputValue(),'9');assert.equal(await hour.isVisible(),true);
  await page.locator('#tab-updates-core').focus();await page.keyboard.press('End');
  assert.equal(await page.locator('#update-form').isVisible(),true);
  await page.keyboard.press('Home');assert.equal(await page.locator('#github-check').isVisible(),true);
  await page.locator('[data-view=backups]').click();
  assert(!reads.includes('storage'));
  await page.locator('#tab-backups-transfer').click();assert.equal(await page.locator('#import-form').isVisible(),true);
  await page.locator('#tab-backups-files').click();await page.waitForFunction(()=>!viewBusy);
  assert(reads.includes('storage'));assert.equal(await page.getByRole('heading',{name:'Автоочистка через неделю',exact:true}).isVisible(),true);
  const preferenceReads=reads.filter(x=>x==='preferences').length;
  await page.locator('[data-view=servers]').click();await page.locator('#tab-servers-backup').click();
  await page.getByText('Не настроен',{exact:true}).waitFor();
  assert.equal(reads.filter(x=>x==='preferences').length,preferenceReads,'profile page must not load monitor settings');
  assert.equal(await page.getByRole('heading',{name:'Резервный VPN-канал',exact:true}).isVisible(),true);
  await page.locator('[data-view=stability]').click();await page.locator('#tab-stability-monitor').click();
  await page.waitForFunction(()=>!viewBusy);
  assert.equal(await page.getByRole('checkbox',{name:'Автоматически переключаться на проверенный резерв'}).isDisabled(),true);
  const monitorInterval=page.locator('#stability-monitor').getByLabel('Интервал в секундах: 120–3600',{exact:true});
  await monitorInterval.fill('600');
  await page.locator('[data-view=servers]').click();
  await page.locator('[data-view=stability]').click();await page.waitForFunction(()=>!viewBusy);
  assert.equal(await monitorInterval.inputValue(),'600','profile refresh must preserve a monitor draft');
  await page.locator('#tab-stability-recovery').click();assert.equal(await page.getByRole('button',{name:'Только DNS helper',exact:true}).isVisible(),true);
  // Finish the latest destination after an earlier, slow page read.
  await page.route('**/api/github',async r=>{await new Promise(resolve=>setTimeout(resolve,250));await r.fulfill({json:{configured:true,current:'1.7.0'}})});
  await page.locator('[data-view=updates]').click();await page.locator('[data-view=backups]').click();
  const before=reads.filter(x=>x==='storage').length;
  await page.waitForFunction(()=>!viewBusy);assert(reads.filter(x=>x==='storage').length>before);
  assert.equal(await page.locator('.nav[aria-current=page]').getAttribute('data-view'),'backups');
  await page.locator('[data-view=sites]').click();await page.locator('#sites-timers summary').click();
  assert.equal(await page.getByRole('button',{name:'Включить временно',exact:true}).isVisible(),true);
  // Every moved feature remains reachable, readable and within the viewport.
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:900});
    for(const view of ['updates','servers','diagnostics','stability','tuning','backups','settings','sites']){
      await page.locator(`[data-view=${view}]`).click();
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,view+' overflow '+width);
    }
    await page.locator('[data-view=updates]').click();await page.locator('#tab-updates-panel').click();
    await page.screenshot({path:`/tmp/darkls-170-updates-${width}.png`,fullPage:true});
  }
  assert.deepEqual(errors,[]);
  console.log(JSON.stringify({result:'PASS',checks:['unique controls','grouped menu','updates together','tab-specific reads','keyboard tabs','draft survives navigation','backup channel','monitor controls','storage and import','latest destination refresh','timer reachable','desktop and mobile layout'],errors}));
  await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
