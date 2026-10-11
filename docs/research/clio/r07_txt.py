import sys,re,html
t=open(sys.argv[1],errors="ignore").read()
imgs=[i for i in re.findall(r'<img[^>]+src="([^"]+)"',t) if "article_attachments" in i or "attachments" in i]
t=re.sub(r"<script.*?</script>|<style.*?</style>","",t,flags=re.S)
t=re.sub(r"</?(strong|em|span|a|b|code)[^>]*>","",t)
x=html.unescape(re.sub(r"<[^>]+>","\n",t)); x=re.sub(r"[ \t]+"," ",x); x=re.sub(r"\n\s*\n+","\n",x)
i=x.find("Enter now\n",x.find("Refer now\n -->\nWin"))
print(x[i:i+int(sys.argv[2])] if i>0 else x[:int(sys.argv[2])])
print("IMGS:",imgs[:12])
