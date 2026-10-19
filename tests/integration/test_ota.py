from pathlib import Path
import tempfile,subprocess,threading,os,time,json,re,zipfile,io,hashlib,http.client,sys,signal
project=Path(os.environ.get('DARKLS_PROJECT_ROOT',Path(__file__).resolve().parents[2]));jobpath=Path('/tmp/darkls-update-status.json');savedjob=jobpath.read_bytes() if jobpath.exists() else None
baseline=Path(os.environ.get('DARKLS_BASELINE_BINARY','/tmp/darkls141-baseline'))
current=Path(os.environ.get('DARKLS_CURRENT_BINARY','/tmp/darkls-current-host'))
old_version=subprocess.check_output([str(baseline),'--version'],text=True).strip()
new_version=subprocess.check_output([str(current),'--version'],text=True).strip()
with tempfile.TemporaryDirectory() as tmp:
 root=Path(tmp)/'xray';panel=root/'panel';panel.mkdir(parents=True)
 for name,data in {'domains.txt':'youtube.com\napi.ipify.org\n','dnsmasq-vpn.conf':'port=10535\n','telegram-networks.txt':'149.154.160.0/20\n','watch.sh':'# DARKLS_PANEL_INTEGRATION\n','vpn.sh':'#!/bin/sh\nBASE=/jffs/xray\niptables -t filter -I INPUT 1 -j DV_IN\n'}.items():(root/name).write_text(data)
 (root/'autostart.disabled').touch()
 mocks=Path(tmp)/'mocks';mocks.mkdir();(mocks/'dnsmasq').write_text('#!/bin/sh\nexit 0\n');(mocks/'dnsmasq').chmod(0o700);os.environ['PATH']=str(mocks)+':'+os.environ['PATH']
 (panel/'darkls-panel').write_bytes(baseline.read_bytes());(panel/'darkls-panel').chmod(0o700)
 initial=subprocess.check_output([str(panel/'darkls-panel'),'--root',str(root),'--listen','127.0.0.1:18091','--init'],text=True);password=re.search(r'Пароль: (\S+)',initial)[1]
 (panel/'service.sh').write_text('''#!/bin/sh
case "$1" in
stop) if [ -s /tmp/darkls-panel.pid ]; then kill "$(cat /tmp/darkls-panel.pid)" 2>/dev/null || true; rm -f /tmp/darkls-panel.pid; sleep 0.2; fi ;;
*) (trap '' HUP; exec "'''+str(panel/'darkls-panel')+'" --root "'+str(root)+'''" --listen 127.0.0.1:18091) >/tmp/darkls-ota-server.log 2>&1 < /dev/null & ;;
esac
''')
 usb=Path(tmp)/'usb.sh';usb.write_text('sh "'+str(panel/'service.sh')+'" ensure\n')
 Path('/tmp/darkls-panel.pid').unlink(missing_ok=True)
 old=subprocess.Popen([str(panel/'darkls-panel'),'--root',str(root),'--listen','127.0.0.1:18091'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 threading.Thread(target=old.wait,daemon=True).start()
 def get(path):
  c=http.client.HTTPConnection('127.0.0.1',18091,timeout=5);c.request('GET',path);r=c.getresponse();d=json.loads(r.read());c.close();return d
 try:
  for i in range(100):
   try:d=get('/api/update-status');break
   except Exception:time.sleep(.03)
  c=http.client.HTTPConnection('127.0.0.1',18091,timeout=60);headers={'Origin':'http://127.0.0.1:18091','Content-Type':'application/json'};c.request('POST','/api/login',json.dumps({'user':'admin','password':password}),headers);r=c.getresponse();data=json.loads(r.read());assert r.status==200;cookie=r.getheader('Set-Cookie').split(';')[0];csrf=data['csrf']
  files={'darkls-panel':current.read_bytes()}
  for name in ['install-update.sh','reload-dns.sh','devices-sync.sh','watch.sh']:
   s=(project/'release'/name).read_text().replace('BASE=/jffs/xray','BASE='+str(root)).replace('/jffs/scripts/usb-mount-script',str(usb)).replace('"$PANEL/darkls-panel" --update-catalog','"$PANEL/darkls-panel" --root "$BASE" --update-catalog').replace('"$PANEL/darkls-panel" --repair-instagram','"$PANEL/darkls-panel" --root "$BASE" --repair-instagram').replace('"$PANEL/darkls-panel" --sync-devices','"$PANEL/darkls-panel" --root "$BASE" --sync-devices')
   if name=='install-update.sh':
    s=s.replace('\"$PANEL/darkls-panel\" --upgrade-policy','true # native migration covered by Go fault-injection tests')
    s=s.replace('"$PANEL/darkls-panel" --enable-x-grok','touch "$PANEL/x-grok-enabled-v1" # native activation covered by Go rollback tests')
    a=s.index('ready() {');b=s.index('\nstart_and_check()',a)
    s=s[:a]+'''ready() {
"'''+sys.executable+'''" -c 'import urllib.request,json;assert json.load(urllib.request.urlopen("http://127.0.0.1:18091/api/update-status",timeout=1))["version"]=="'''+new_version+'''"'
}
'''+s[b:]
    s='\n'.join('  true # no external probe in OTA fixture' if "(trap '' HUP;" in line else line for line in s.splitlines())+'\n'
   files[name]=s.encode()
  m={'format':1,'version':new_version,'arch':'amd64','files':{name:{'sha256':hashlib.sha256(b).hexdigest(),'size':len(b)} for name,b in files.items()}}
  buf=io.BytesIO()
  with zipfile.ZipFile(buf,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
   for name,b in files.items():z.writestr('darkls-panel-update/'+name,b)
   z.writestr('darkls-panel-update/manifest.json',json.dumps(m))
  boundary='DARKLS_TEST_UPLOAD_BOUNDARY';body=(f'--{boundary}\r\nContent-Disposition: form-data; name="password"\r\n\r\n{password}\r\n--{boundary}\r\nContent-Disposition: form-data; name="package"; filename="test.zip"\r\nContent-Type: application/zip\r\n\r\n').encode()+buf.getvalue()+f'\r\n--{boundary}--\r\n'.encode()
  headers={'Origin':'http://127.0.0.1:18091','X-CSRF-Token':csrf,'Cookie':cookie,'Content-Type':'multipart/form-data; boundary='+boundary}
  c.request('POST','/api/update',body,headers);r=c.getresponse();result=json.loads(r.read());assert r.status==202,(r.status,result);c.close()
  deadline=time.monotonic()+60
  while time.monotonic()<deadline:
   try:
    d=get('/api/update-status');phase=d['job']['phase']
    if phase in ['error','rolled_back']:raise AssertionError(d)
    if phase=='succeeded':break
   except (ConnectionError,OSError,http.client.HTTPException):pass
   time.sleep(.2)
  else:raise AssertionError('OTA timeout '+str(d))
  assert d['version']==new_version,d
  c=http.client.HTTPConnection('127.0.0.1',18091,timeout=5);c.request('POST','/api/login',json.dumps({'user':'admin','password':password}),{'Origin':'http://127.0.0.1:18091','Content-Type':'application/json'});r=c.getresponse();r.read();assert r.status==200;c.close()
  assert (panel/'darkls-panel.previous').exists();print('Authenticated OTA upload -> staging -> separate worker -> server restart -> new version -> unchanged password: PASS')
 finally:
  pidfile=Path('/tmp/darkls-panel.pid')
  if pidfile.exists():
   try:os.kill(int(pidfile.read_text()),signal.SIGTERM)
   except (ProcessLookupError,ValueError):pass
   pidfile.unlink(missing_ok=True)
  if old.poll() is None:old.terminate();old.wait(timeout=5)
  if savedjob is None:jobpath.unlink(missing_ok=True)
  else:jobpath.write_bytes(savedjob)
