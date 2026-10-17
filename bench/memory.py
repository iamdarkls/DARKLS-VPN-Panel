import tempfile,pathlib,subprocess,time,http.client,json,re,statistics,os

def run(binary):
 with tempfile.TemporaryDirectory() as tmp:
  root=pathlib.Path(tmp)
  for name,data in {'domains.txt':'youtube.com\napi.ipify.org\n','dnsmasq-vpn.conf':'port=10535\n','telegram-networks.txt':'149.154.160.0/20\n'}.items():(root/name).write_text(data)
  init=subprocess.run([binary,'--root',tmp,'--listen','127.0.0.1:18089','--init'],capture_output=True,text=True,check=True)
  password=re.search(r'Пароль: (\S+)',init.stdout)[1]
  proc=subprocess.Popen([os.environ.get('DARKLS_RSS_SUPERVISOR','./bench/rss-supervisor'),binary,'--root',tmp,'--listen','127.0.0.1:18089'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
  try:
   for i in range(100):
    try:
     c=http.client.HTTPConnection('127.0.0.1',18089,timeout=5);c.request('GET','/');r=c.getresponse();r.read();break
    except OSError:time.sleep(.02)
   headers={'Origin':'http://127.0.0.1:18089','Content-Type':'application/json'}
   c.request('POST','/api/login',json.dumps({'user':'admin','password':password}),headers);r=c.getresponse();r.read();assert r.status==200
   headers['Cookie']=r.getheader('Set-Cookie').split(';')[0]
   start=time.monotonic()
   for i in range(1200):
    path=['/api/state','/api/export','/api/backups'][i%3]
    c.request('GET',path,headers=headers);r=c.getresponse();r.read();assert r.status==200,(path,r.status)
   elapsed=time.monotonic()-start;c.close()
   proc.terminate();output,_=proc.communicate(timeout=5)
   return {'peak_rss_kib':int(output.strip()),'requests':1200,'seconds':round(elapsed,3)}
  finally:
   if proc.returncode is None:proc.terminate();proc.wait(timeout=5)
result={name:[run(binary) for _ in range(3)] for name,binary in [('baseline_1.0.2',os.environ.get('DARKLS_BASELINE_BINARY','/tmp/darkls-baseline-host')),('optimized_1.0.3',os.environ.get('DARKLS_OPTIMIZED_BINARY','/tmp/darkls-optimized-host'))]}
pathlib.Path('bench/results.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result,indent=2))
