const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/session',async route=>{const response=await route.fetch();const data=await response.json();await route.fulfill({response,json:{...data,user:'actual-session-user'}});});
 await page.goto('http://127.0.0.1:18088');
 assert.equal(await page.locator('#login-form input[name=user]').getAttribute('autocomplete'),'username');
 await page.locator('#login-form input[name=password]').fill('preview-password');await page.locator('#login-form input[name=password]').press('Enter');
 await page.locator('.card').first().waitFor();
 const nav=page.getByLabel('Поиск раздела',{exact:true});assert.equal(await nav.inputValue(),'');
 const snapshot=await page.evaluate(()=>{
  const searches=[...document.querySelectorAll('#app input[type=search]')];
  const passwords=[...document.querySelectorAll('#app input[type=password]')];
  return {searches:searches.map(input=>({name:input.name,autocomplete:input.autocomplete,role:input.form?.getAttribute('role'),passwords:input.form?.querySelectorAll('input[type=password]').length})),
   passwords:passwords.map(input=>({name:input.name,autocomplete:input.autocomplete,user:input.form?.querySelector('input[autocomplete=username]')?.value})),nested:document.querySelectorAll('form form').length};
 });
 assert.ok(snapshot.searches.length>=2);assert.equal(snapshot.nested,0);
 for(const field of snapshot.searches){assert.ok(field.name);assert.equal(field.autocomplete,'off');assert.equal(field.role,'search');assert.equal(field.passwords,0);}
 assert.ok(snapshot.passwords.length>=10);
 for(const field of snapshot.passwords){assert.ok(field.name);assert.equal(field.user,'actual-session-user');assert.ok(['current-password','new-password'].includes(field.autocomplete));}
 let posts=0;page.on('request',r=>{if(r.method()==='POST')posts++;});
 await nav.fill('Обновления');await nav.press('Enter');assert.equal(await page.locator('.nav').count(),1);assert.equal(posts,0);await nav.fill('');
 await page.locator('[data-view=updates]').click();await page.locator('#github-channel-password').fill('confirmation-secret');
 assert.equal(await nav.inputValue(),'');assert.ok(await page.locator('.nav').count()>5);
 assert.equal(await page.locator('#github-channel-password').evaluate(n=>n.form.querySelector('input[autocomplete=username]').value),'actual-session-user');
 await page.locator('[data-view=sites]').click();await page.locator('#search').fill('YouTube');await page.locator('#search').press('Enter');assert.equal(posts,0);await page.locator('#search').fill('');
 await page.locator('#open-logs').click();const logs=page.getByRole('dialog',{name:'Журналы',exact:true});await logs.waitFor();
 assert.equal(await page.getByLabel('Поиск в журнале',{exact:true}).evaluate(n=>n.form.getAttribute('role')),'search');await page.keyboard.press('Escape');
 await page.locator('#logout').click();await page.locator('#login-form').waitFor();assert.equal(await page.locator('#app input[autocomplete=username]').count(),0);
 assert.equal(await page.evaluate(()=>Object.values(localStorage).some(v=>v.includes('confirmation-secret')||v.includes('actual-session-user'))),false);
 assert.deepEqual(errors,[]);await browser.close();
 console.log('PASS: isolated search forms, real session username for every password confirmation, login Enter, search Enter, navigation and session cleanup (form semantics; native password-store autofill not simulated)');
})().catch(e=>{console.error(e);process.exit(1)});
