import sys,json,re,html
d=json.load(open(sys.argv[1]))["article"]
b=d["body"]
imgs=re.findall(r'src="([^"]+)"',b)
b=re.sub(r'<(h\d)[^>]*>',r'\n## ',b); b=re.sub(r'<(li|p|tr|br|td|th)[^>]*>',r'\n',b)
b=re.sub(r'<[^>]+>',' ',b); b=html.unescape(b); b=re.sub(r'[ \t]+',' ',b); b=re.sub(r'\n\s*\n+','\n',b)
print("TITLE:",d["title"],"|",d["html_url"],"| updated",d["updated_at"])
print(b[:int(sys.argv[2]) if len(sys.argv)>2 else 12000])
print("IMGS:",len(imgs), imgs[:4])
