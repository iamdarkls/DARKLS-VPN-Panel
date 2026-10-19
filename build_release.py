#!/usr/bin/env python3
"""Build the compact Linux runtime package accepted by both SSH and panel upload."""
import argparse,hashlib,json,os,pathlib,re,subprocess,zipfile
root=pathlib.Path(__file__).resolve().parent
parser=argparse.ArgumentParser();parser.add_argument('--version',default='1.6.0');parser.add_argument('--arch',default='arm64',choices=['arm64','amd64']);parser.add_argument('--output-dir',type=pathlib.Path,default=root/'dist');args=parser.parse_args()
if not re.fullmatch(r'\d{1,4}\.\d{1,4}\.\d{1,4}',args.version):raise SystemExit('invalid release version')
env={**os.environ,'CGO_ENABLED':'0','GOOS':'linux','GOARCH':args.arch};binary=root/'release/darkls-panel'
subprocess.run([os.environ.get('GO_BIN','go'),'build','-buildvcs=false','-trimpath','-gcflags=all=-l','-ldflags=-s -w -buildid= -X main.version='+args.version,'-o',str(binary),'.'],cwd=root,env=env,check=True)
names=['darkls-panel','install-update.sh','reload-dns.sh','devices-sync.sh','watch.sh']
manifest={'format':1,'version':args.version,'arch':args.arch,'files':{}}
for name in names:
 data=(root/'release'/name).read_bytes();manifest['files'][name]={'sha256':hashlib.sha256(data).hexdigest(),'size':len(data)}
(root/'release/manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
args.output_dir.mkdir(parents=True,exist_ok=True);out=args.output_dir/f'DARKLS-VPN-Panel-v{args.version}.zip'
with zipfile.ZipFile(out,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
 for name in names+['manifest.json']:z.write(root/'release'/name,'darkls-panel-update/'+name)
print(json.dumps({'path':str(out.resolve()),'bytes':out.stat().st_size,'binary_bytes':binary.stat().st_size,'sha256':hashlib.sha256(out.read_bytes()).hexdigest()}))
