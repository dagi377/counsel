import sys,json
d=json.load(open(sys.argv[1]))
for r in d.get("results",[]): print(r["id"], r["title"])
