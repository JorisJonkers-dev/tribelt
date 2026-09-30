import re, json, time, urllib.request, html, sys
from html.parser import HTMLParser
D=sys.argv[1]
BASE="https://www.tribelt.nl"
sm=open(D+"/sitemap.xml").read()
queue=sorted(set(u.replace(BASE,"") or "/" for u in re.findall(r"<loc>([^<]+)</loc>",sm)))
seen=set(); out={}
class P(HTMLParser):
    def __init__(s):
        super().__init__(); s.stack=[]; s.h={'h1':[],'h2':[],'h3':[]}; s.cur=None; s.buf=''; s.links=set(); s.ld=[]; s.inld=False; s.ldbuf=''; s.forms=[]; s.text=[]; s.skip=0; s.pbuf=None
    def handle_starttag(s,t,a):
        a=dict(a)
        if t in s.h: s.cur=t; s.buf=''
        if t=='a' and a.get('href'): s.links.add(a['href'])
        if t=='script' and a.get('type')=='application/ld+json': s.inld=True; s.ldbuf=''
        elif t in('script','style','noscript'): s.skip+=1
        if t=='form': s.forms.append(a.get('name') or a.get('id') or a.get('action') or 'form')
        if t=='p': s.pbuf=''
    def handle_endtag(s,t):
        if t==s.cur: s.h[t].append(re.sub(r'\s+',' ',s.buf).strip()); s.cur=None
        if t=='script' and s.inld: s.ld.append(s.ldbuf); s.inld=False
        elif t in('script','style','noscript') and s.skip: s.skip-=1
        if t=='p' and s.pbuf is not None:
            x=re.sub(r'\s+',' ',s.pbuf).strip()
            if len(x)>40: s.text.append(x)
            s.pbuf=None
    def handle_data(s,d):
        if s.inld: s.ldbuf+=d; return
        if s.skip: return
        if s.cur: s.buf+=d
        if s.pbuf is not None: s.pbuf+=d
while queue:
    p=queue.pop(0)
    p=p.split('#')[0].split('?')[0].rstrip('/') or '/'
    if p in seen: continue
    seen.add(p)
    try:
        import subprocess
        res=subprocess.run(['curl','-sL','-A','Mozilla/5.0','-w','\n@@%{http_code} %{url_effective}','--max-time','20',BASE+p],capture_output=True)
        raw=res.stdout.decode('utf-8','replace'); body,_,meta=raw.rpartition('\n@@'); code,final=meta.split(' ',1); code=int(code)
        final=final.replace(BASE,'') or '/'
    except Exception as e:
        out[p]={'error':str(e)}; continue
    pr=P(); pr.feed(body)
    g=lambda rx:(m.group(1) if (m:=re.search(rx,body,re.I|re.S)) else None)
    rec={'code':code,'final':final,'title':html.unescape(g(r'<title>(.*?)</title>') or ''),
      'desc':html.unescape(g(r'<meta content="([^"]*)" name="description"') or g(r'<meta name="description" content="([^"]*)"') or ''),
      'lang':g(r'<html[^>]*lang="([^"]*)"'),'canonical':g(r'<link[^>]*rel="canonical"[^>]*href="([^"]*)"') or g(r'<link href="([^"]*)" rel="canonical"'),
      'hreflang':re.findall(r'hreflang="([^"]+)"',body),'robots':g(r'<meta[^>]*name="robots"[^>]*content="([^"]*)"') or g(r'<meta content="([^"]*)" name="robots"'),
      'og':re.findall(r'property="(og:[a-z]+)"',body),
      'h1':pr.h['h1'],'h2':pr.h['h2'],'h3':pr.h['h3'][:12],'ld':pr.ld,'forms':pr.forms,'text':[html.unescape(t) for t in pr.text[:8]],
      'ext':sorted(l for l in pr.links if l.startswith('http') and 'tribelt.nl' not in l)}
    out[p]=rec
    for l in pr.links:
        l=l.replace(BASE,'')
        if l.startswith('/') and not l.startswith('//'):
            l=l.split('#')[0].split('?')[0].rstrip('/') or '/'
            if l not in seen and l not in queue and not re.search(r'\.(pdf|png|jpg|css|js)$',l): queue.append(l)
    time.sleep(0.3)
json.dump(out,open(D+"/pages.json","w"),indent=1,ensure_ascii=False)
print(len(out))
