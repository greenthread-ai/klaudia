#!/usr/bin/env python3
"""Merge Go text coverage profiles and print a per-package coverage table.

A statement counts as covered if any profile covered it. Used by scripts/cover.sh.

Usage: covtable.py merged.out name=profile.out [name=profile.out ...]"""
import sys, collections
out, specs = sys.argv[1], [a.split('=',1) for a in sys.argv[2:]]
def load(f):
    d = {}
    for l in open(f).readlines()[1:]:
        loc, n, c = l.split()
        d[loc] = (int(n), d.get(loc, (0, False))[1] or int(c) > 0)
    return d
profs = {name: load(f) for name, f in specs}
blocks = {}
for d in profs.values():
    for k, (n, c) in d.items():
        blocks[k] = (n, blocks.get(k, (0, False))[1] or c)
with open(out, 'w') as w:
    w.write('mode: set\n')
    for k, (n, c) in blocks.items(): w.write(f'{k} {n} {int(c)}\n')
pkg = lambda k: k.split(':')[0].split('klaudia/', 1)[1].rsplit('/', 1)[0]
den = collections.Counter(); hit = {n: collections.Counter() for n in list(profs) + ['all']}
for k, (n, c) in blocks.items():
    den[pkg(k)] += n
    if c: hit['all'][pkg(k)] += n
for name, d in profs.items():
    for k, (n, c) in d.items():
        if c: hit[name][pkg(k)] += n
cols = list(profs) + ['all']
print(f"{'package':26}" + ''.join(f'{c:>8}' for c in cols) + f"{'stmts':>7}")
for p in sorted(den, key=lambda p: hit['all'][p] / den[p]):
    print(f'{p:26}' + ''.join(f'{100*hit[c][p]/den[p]:7.1f}%' for c in cols) + f'{den[p]:7}')
T = sum(den.values())
print(f"{'TOTAL':26}" + ''.join(f'{100*sum(hit[c].values())/T:7.1f}%' for c in cols) + f'{T:7}')
