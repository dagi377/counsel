import sys,re,html
t=open(sys.argv[1],errors="ignore").read()
imgs=set(re.findall(r'(?:src|href)="([^"]+\.(?:png|jpe?g|gif|webp|mp4)[^"]*)"',t))
m=re.search(r'<article.*?</article>',t,re.S) or re.search(r'<main.*?</main>',t,re.S)
b=m.group(0) if m else t
b=re.sub(r'<(script|style)[^>]*>.*?</\1>','',b,flags=re.S)
b=re.sub(r'<(h\d|li|p|tr|br)[^>]*>',r'\n',b)
b=re.sub(r'<[^>]+>',' ',b); b=html.unescape(b); b=re.sub(r'[ \t]+',' ',b); b=re.sub(r'\n\s*\n+','\n',b)
print(b[:20000]); print("IMAGES:"); [print(i) for i in sorted(imgs) if 'clio' in i or 'zendesk' in i]
