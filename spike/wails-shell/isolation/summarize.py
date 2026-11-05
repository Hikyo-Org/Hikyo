#!/usr/bin/env python3
# usage: summarize.py RUN attacker.jsonl victim.jsonl [shell.log]
# One row per vector: requests that reached the victim front (the ground truth), with their bearer, and what the page saw.
import json, sys, urllib.parse as up
from collections import defaultdict
import re
run, attacker, victim = sys.argv[1:4]
proxy = defaultdict(list)
if len(sys.argv) > 4:
    for line in open(sys.argv[4], errors="replace"):
        m = re.search(r"(refused[^/]*?)\s\S*\s*(?:GET|POST|OPTIONS|HEAD)?\s*/v/([\w-]+)", line) or re.search(r"(refused \S+ \S+ \S+) \S+ \S+ /v/([\w-]+)", line)
        if m: proxy[m.group(2)].append("refused-origin" if "cross-connection" in line else "refused-host" if "unknown host" in line else "refused-window")
        m = re.search(r"native: navigation \S+/v/([\w-]+)\?\S* \(.*main=(\d) new=(\d)\) -> (\w+)", line)
        if m: proxy[m.group(1)].append(f"native-{m.group(4)}")
        m = re.search(r"NAV-(REFUSED|ALLOWED) \S+/v/([\w-]+)", line)
        if m: proxy[m.group(2)].append("native-cancel" if m.group(1) == "REFUSED" else "native-allow")
hits, seen = defaultdict(list), defaultdict(list)
def rows(p):
    for line in open(p):
        try: yield json.loads(line)
        except ValueError: pass
for e in rows(victim):
    u = up.urlsplit(e["uri"]); q = up.parse_qs(u.query)
    if q.get("run", [""])[0] != run: continue
    if u.path.startswith("/v/"): hits[u.path[3:]].append(e)
    if u.path == "/report" and q.get("v"): seen[q["v"][0]].append("victim-side:" + q.get("r", [""])[0])
for e in rows(attacker):
    u = up.urlsplit(e["uri"]); q = up.parse_qs(u.query)
    if q.get("run", [""])[0] != run: continue
    if u.path == "/report": seen[q["v"][0]].append(q.get("r", [""])[0])
    if u.path.startswith("/v/"): seen[u.path[3:]].append("own-front-hit bearer=" + e["bearer"])
print("| Vector | Reached victim | Method | Bearer | Shell | Page saw |\n|---|---|---|---|---|---|")
for v in sorted(set(hits) | set(seen) | set(proxy)):
    h = hits.get(v, [])
    print(f"| {v} | {len(h)} | {','.join(sorted({e['method'] for e in h}))} | {','.join(sorted({e['bearer'] or '(none)' for e in h}))} | {','.join(sorted(set(proxy.get(v, []))))} | {'; '.join(seen.get(v, []))[:140]} |")
