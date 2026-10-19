from pathlib import Path
import tempfile,subprocess,shutil,json,os,hashlib
project=Path(os.environ.get('DARKLS_PROJECT_ROOT',Path(__file__).resolve().parents[2]))
baseline=Path(os.environ.get('DARKLS_BASELINE_BINARY','/tmp/darkls141-baseline'))
current=Path(os.environ.get('DARKLS_CURRENT_BINARY','/tmp/darkls-current-host'))
old_version=subprocess.check_output([str(baseline),'--version'],text=True).strip()
new_version=subprocess.check_output([str(current),'--version'],text=True).strip()
for fail_first in [False,True]:
 with tempfile.TemporaryDirectory() as tmp:
  home=Path(tmp);root=home/'xray';panel=root/'panel';panel.mkdir(parents=True);temp=home/'tmp';temp.mkdir();pkg=temp/'darkls-panel-update';pkg.mkdir()
  originals={'domains.txt':'youtube.com\napi.ipify.org\n','dnsmasq-vpn.conf':'port=10535\n','telegram-networks.txt':'149.154.160.0/20\n','watch.sh':'# DARKLS_PANEL_INTEGRATION\n','vpn.sh':'#!/bin/sh\nBASE=/jffs/xray\niptables -t filter -I INPUT 1 -j DV_IN\n'}
  for name,data in originals.items():(root/name).write_text(data)
  old_state=json.dumps({'version':1,'groups':[{'id':'youtube','name':'Мой YouTube','enabled':False,'domains':['youtube.com']},{'id':'custom','name':'Мои сайты','enabled':True,'domains':['api.ipify.org']}]})
  (panel/'state.json').write_text(old_state);(panel/'auth.json').write_text('{"preserved":true}')
  (panel/'service.sh').write_text('rm -f "'+str(temp/'darkls-panel.pid')+'"\n')
  shutil.copy(baseline,panel/'darkls-panel')
  (panel/'test-cmdline').write_text('/jffs/xray/panel/darkls-panel')
  startup=home/'usb.sh'
  startup.write_text('''#!/bin/sh
n=0
[ ! -f "'''+str(home/'attempts')+'''" ] || n=$(cat "'''+str(home/'attempts')+'''" )
n=$((n+1));echo "$n" > "'''+str(home/'attempts')+'''"
'''+('if [ "$n" -eq 1 ]; then exit 0; fi\n' if fail_first else '')+'echo '+str(os.getpid())+' > "'+str(temp/'darkls-panel.pid')+'"\n')
  shutil.copy(current,pkg/'darkls-panel')
  for name in ['install-update.sh','reload-dns.sh','devices-sync.sh','watch.sh']:
   s=(project/'release'/name).read_text().replace('BASE=/jffs/xray','BASE='+str(root)).replace('/jffs/scripts/usb-mount-script',str(startup)).replace('"$PANEL/darkls-panel" --update-catalog','"$PANEL/darkls-panel" --root "$BASE" --update-catalog').replace('"$PANEL/darkls-panel" --repair-instagram','"$PANEL/darkls-panel" --root "$BASE" --repair-instagram').replace('"$PANEL/darkls-panel" --sync-devices','"$PANEL/darkls-panel" --root "$BASE" --sync-devices').replace('"/proc/$current_pid/cmdline"','"$PANEL/test-cmdline"').replace('/tmp/darkls-',str(temp)+'/darkls-').replace('/tmp/xray-arm64.zip',str(temp)+'/xray-arm64.zip').replace('/tmp/asusware-usbmount.zip',str(temp)+'/asusware-usbmount.zip')
   s=s.replace('\"$PANEL/darkls-panel\" --upgrade-policy','true # native migration is covered by Go fault-injection tests')
   s=s.replace('"$PANEL/darkls-panel" --enable-x-grok','touch "$PANEL/x-grok-enabled-v1" # native activation covered by Go rollback tests')
   s='\n'.join('  true # probe omitted in fixture' if "(trap '' HUP;" in line else line for line in s.splitlines())+'\n'
   (pkg/name).write_text(s)
  manifest={'format':1,'version':new_version,'arch':'amd64','files':{}}
  for name in ['darkls-panel','install-update.sh','reload-dns.sh','devices-sync.sh','watch.sh']:
   b=(pkg/name).read_bytes();manifest['files'][name]={'sha256':hashlib.sha256(b).hexdigest(),'size':len(b)}
  (pkg/'manifest.json').write_text(json.dumps(manifest))
  mocks=home/'mocks';mocks.mkdir();(mocks/'sleep').write_text('#!/bin/sh\nexit 0\n');(mocks/'sleep').chmod(0o700)
  p=subprocess.run(['sh',str(pkg/'install-update.sh')],env={**os.environ,'PATH':str(mocks)+':'+os.environ['PATH']},capture_output=True,text=True,timeout=20)
  if not fail_first:
   assert p.returncode==0,(p.stdout,p.stderr)
   assert subprocess.check_output([str(panel/'darkls-panel'),'--version'],text=True).strip()==new_version
   state=json.loads((panel/'state.json').read_text());assert state['groups'][0]['name']=='Мой YouTube' and not state['groups'][0]['enabled']
   assert 'DARKLS_DEVICE_POLICY_HOOK' in (root/'vpn.sh').read_text()
   assert (panel/'x-grok-enabled-v1').exists()
   assert not pkg.exists();print('Bootstrap success: PASS, manifest, hooks, user state, cleanup')
  else:
   assert p.returncode!=0,(p.stdout,p.stderr)
   assert subprocess.check_output([str(panel/'darkls-panel'),'--version'],text=True).strip()==old_version
   assert not (panel/'x-grok-enabled-v1').exists()
   assert (panel/'state.json').read_text()==old_state
   assert (root/'vpn.sh').read_text()==originals['vpn.sh']
   assert (pkg/'rollback.ok').exists();print('Bootstrap failed new startup: PASS, old executable/state/hooks restored and old service ready')
