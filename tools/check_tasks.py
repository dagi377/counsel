#!/usr/bin/env python3
"""Validate spec/tasks.json: unique ids, deps exist and sit in earlier waves, no file
overlap inside a wave, every o1 T-id and every AI task and feature covered, at most
MAX_WAVES waves. Prints the critical path (longest dependency chain).
Usage: python3 -I tools/check_tasks.py [repo_root]"""
import json
import sys
from collections import defaultdict

MAX_WAVES = 8


def critical_path(byid):
    """Longest chain of depends_on, ending at the deepest task (ties broken by id)."""
    memo = {}

    def depth(i):
        if i not in memo:
            ds = [d for d in byid[i]['depends_on'] if d in byid]
            memo[i] = 1 + max((depth(d) for d in ds), default=0)
        return memo[i]

    cur = max(byid, key=lambda i: (depth(i), i))
    path = [cur]
    while True:
        ds = [d for d in byid[cur]['depends_on'] if d in byid]
        if not ds:
            return path[::-1]
        cur = max(ds, key=lambda d: (depth(d), d))
        path.append(cur)


def overlap(a, b):
    a, b = a.rstrip('/'), b.rstrip('/')
    return a == b or a.startswith(b + '/') or b.startswith(a + '/')


def main(root):
    errs = []
    plan = json.load(open(f'{root}/spec/tasks.json'))
    tasks = plan['tasks']
    o1 = json.load(open(f'{root}/spec/eng/o1-tasks.json'))['tasks']
    ai = json.load(open(f'{root}/spec/ai/tasks-ai.json'))['tasks']
    feats = json.load(open(f'{root}/spec/ai/features.json'))['features']
    byid = {}
    for t in tasks:
        if t['id'] in byid:
            errs.append(f'duplicate id {t["id"]}')
        byid[t['id']] = t
        for k in ('id', 'wave', 'parent', 'layer', 'title', 'depends_on', 'files', 'rule_ids', 'accept', 'size'):
            if k not in t:
                errs.append(f'{t["id"]}: missing {k}')
        if t.get('size') != 'S':
            errs.append(f'{t["id"]}: size {t.get("size")} != S')
        if not t.get('accept'):
            errs.append(f'{t["id"]}: no accept')
        if not t.get('files'):
            errs.append(f'{t["id"]}: no files')
    for t in tasks:
        for d in t['depends_on']:
            if d not in byid:
                errs.append(f'{t["id"]}: unknown dep {d}')
            elif byid[d]['wave'] >= t['wave']:
                errs.append(f'{t["id"]} (wave {t["wave"]}): dep {d} is wave {byid[d]["wave"]}')
    waves = defaultdict(list)
    for t in tasks:
        waves[t['wave']].append(t)
    for w, ts in waves.items():
        owned = []
        for t in ts:
            for f in t['files']:
                for g, oid in owned:
                    if overlap(f, g) and oid != t['id']:
                        errs.append(f'wave {w}: {t["id"]} file {f} overlaps {oid} {g}')
                owned.append((f, t['id']))
    parents = {t['parent'] for t in tasks}
    for t in o1:
        if t['id'] not in parents:
            errs.append(f'o1 task {t["id"]} not covered')
        mine = {r for x in tasks if x['parent'] == t['id'] for r in x['rule_ids']}
        for r in t['rule_ids']:
            if r not in mine:
                errs.append(f'{t["id"]}: rule {r} not on any child')
    for a in ai:
        if a['id'] not in byid:
            errs.append(f'AI task {a["id"]} missing')
    covered = {t.get('ai_feature') for t in tasks}
    for f in feats:
        if f['id'] not in covered:
            errs.append(f'AI feature {f["id"]} not covered')
    counts = {w: len(ts) for w, ts in sorted(waves.items())}
    if len(counts) > MAX_WAVES:
        errs.append(f'{len(counts)} waves > {MAX_WAVES}')
    for e in errs:
        print('ERROR', e)
    try:
        path = critical_path(byid)
        print(f'critical_path={len(path)} ' + ' -> '.join(path))
    except RecursionError:
        errs.append('dependency cycle')
    print(f'tasks={len(tasks)} waves={len(counts)} per_wave={counts} max_width={max(counts.values())}')
    print('FAIL' if errs else 'OK')
    return 1 if errs else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1] if len(sys.argv) > 1 else '.'))
