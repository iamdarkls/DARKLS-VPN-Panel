const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 const page=await browser.newPage({viewport:{width:1440,height:1100}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 let response;
 await page.route('**/api/state',async route=>{
  const upstream=await route.fetch();response=await upstream.json();
  const group=response.state.groups.find(g=>g.id==='gemini');
  response.catalog_updates=[{id:'gemini',digest:'a'.repeat(64),group,missing:['catalog-check.example'],new:false}];
  await route.fulfill({response:upstream,json:response});
 });
 await page.goto('http://127.0.0.1:18088');
 await page.locator('#login-form input[name=password]').fill('preview-password');await page.locator('#login-form button').click();
 const box=page.locator('.panel-box').filter({has:page.getByRole('heading',{name:'Обновления каталога',exact:true})});
 await box.getByText('Предложений: 1',{exact:true}).waitFor();
 await box.locator('summary').click();await box.getByRole('button',{name:'Отклонить',exact:true}).click();
 await box.getByText('Предложений: 0',{exact:true}).waitFor();
 await page.locator('#discard').click();await box.getByText('Предложений: 1',{exact:true}).waitFor();
 await box.locator('summary').click();await box.getByRole('button',{name:'Добавить в черновик',exact:true}).click();
 await box.getByText('Предложений: 0',{exact:true}).waitFor();
 let captured;
 await page.route('**/api/preview',async route=>{captured=route.request().postDataJSON();await route.fulfill({json:{added_domains:['catalog-check.example'],removed_domains:[],groups:[],old_device_mode:'all',new_device_mode:'all',note:'test preview'}})});
 await page.locator('#apply').click();await page.waitForFunction(()=>document.querySelector('dialog[open]'));
 assert.equal(captured.state.catalog_choices.gemini,'a'.repeat(64));
 const after=captured.state.groups.find(g=>g.id==='gemini');
 assert.ok(after.domains.includes('catalog-check.example'));assert.equal(after.enabled,response.state.groups.find(g=>g.id==='gemini').enabled);
 assert.deepEqual(errors,[]);await browser.close();console.log('PASS: per-group catalog decline/undo/accept, persisted decision, normal apply preview, selection preserved');
})().catch(e=>{console.error(e);process.exit(1)});
