#!/usr/bin/env python3
"""MineWatch heartbeat watchdog. Loopback only; paired with self-hosted Bark."""
import hashlib,hmac,json,os,re,threading,time,urllib.request,urllib.parse
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
from pathlib import Path

class Watchdog:
    def __init__(self,path,sender=None):
        self.path=Path(path);self.lock=threading.RLock();self.nodes={};self.sender=sender or self.push
        if self.path.exists():
            try:self.nodes=json.loads(self.path.read_text())
            except (ValueError,OSError):raise RuntimeError('Invalid watchdog state; refusing to discard alerts')
    def save(self):
        self.path.parent.mkdir(parents=True,exist_ok=True)
        tmp=self.path.with_suffix('.tmp');tmp.write_text(json.dumps(self.nodes));os.chmod(tmp,0o600);os.replace(tmp,self.path)
    def heartbeat(self,key,payload,now):
        if not re.fullmatch(r'[A-Za-z0-9_-]{16,256}',key):raise ValueError('invalid key')
        identity=payload.get('id','')
        if not re.fullmatch(r'[a-f0-9]{32}',identity):raise ValueError('invalid id')
        if type(payload.get('enabled',True)) is not bool or type(payload.get('paused',False)) is not bool:raise ValueError('invalid flag')
        uid=hashlib.sha256((key+':'+identity).encode()).hexdigest()
        with self.lock:
            self.nodes={k:v for k,v in self.nodes.items() if now-v['last']<3*86400}
            if uid not in self.nodes and len(self.nodes)>=64:raise ValueError('client limit')
            n=self.nodes.get(uid,{'notified':False,'retry':0})
            if 'last' in n and now-n['last']<10 and payload.get('enabled',True)==n['enabled']:return
            n.update(key=key,name='矿场监控电脑',last=now,enabled=payload.get('enabled',True),paused=payload.get('paused',False))
            if not n['enabled']:n['notified']=False
            self.nodes[uid]=n;self.save()
    def check(self,now):
        with self.lock:targets=[(k,dict(v)) for k,v in self.nodes.items()]
        for uid,n in targets:
            if not n['enabled'] or now<n.get('retry',0):continue
            offline=now-n['last']>1500
            if offline==n['notified']:continue
            title='监控程序失联' if offline else '监控程序恢复'
            body=('超过25分钟未收到监控电脑心跳。可能是电脑断电、网络中断或程序停止，无法据此判定矿机掉线。' if offline else '已重新收到监控电脑心跳。')
            body+='\n最后心跳：'+time.strftime('%m-%d %H:%M:%S',time.gmtime(n['last']+8*3600))+'（北京时间）'
            with self.lock:
                current=self.nodes.get(uid)
                if not current or current['last']!=n['last'] or not current['enabled']:continue
            ok=self.sender(n['key'],'矿场视窗 · '+title,body)
            with self.lock:
                current=self.nodes.get(uid)
                if not current or current['last']!=n['last']:continue
                current['retry']=now+60
                if ok:current['notified']=offline
                self.save()
    def push(self,key,title,body):
        try:
            data=urllib.parse.urlencode({'title':title,'body':body,'group':'矿场视窗'}).encode()
            req=urllib.request.Request('http://127.0.0.1:8788/minewatch-bark/'+key,data=data,method='POST')
            with urllib.request.urlopen(req,timeout=10) as response:
                return response.status==200 and json.loads(response.read(8192)).get('code')==200
        except Exception:return False

class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_GET(self):
        self.send_response(200 if self.path=='/minewatch-watchdog/health' else 404);self.end_headers();self.wfile.write(b'{"service":"minewatch-watchdog","ok":true}')
    def do_POST(self):
        try:
            if self.path!='/minewatch-watchdog/heartbeat':raise ValueError('path')
            length=int(self.headers.get('Content-Length','0'))
            if not 0<length<=4096:raise ValueError('size')
            auth=self.headers.get('Authorization','')
            if not auth.startswith('Bearer '):raise ValueError('auth')
            payload=json.loads(self.rfile.read(length))
            if not isinstance(payload,dict):raise ValueError('body')
            self.server.watchdog.heartbeat(auth[7:],payload,time.time())
            self.send_response(200);self.end_headers();self.wfile.write(b'{"ok":true}')
        except Exception:
            self.send_response(400);self.end_headers();self.wfile.write(b'{"ok":false}')

def main():
    state=os.environ.get('STATE_DIRECTORY','/var/lib/minewatch-watchdog')
    watch=Watchdog(Path(state)/'clients.json')
    server=ThreadingHTTPServer(('127.0.0.1',8790),Handler);server.daemon_threads=True;server.watchdog=watch
    def loop():
        time.sleep(660) # Allow running clients to renew after server restart.
        while True:
            try:watch.check(time.time())
            except Exception:pass
            time.sleep(15)
    threading.Thread(target=loop,daemon=True).start();server.serve_forever()
if __name__=='__main__':main()
