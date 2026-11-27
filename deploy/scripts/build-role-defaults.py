#!/usr/bin/env python3
"""Aggregate roles/<role>/defaults/main.yml into group_vars/all.yml.

Role defaults are the single source of truth (ADR-63). Ansible only auto-loads a
role's defaults when that role is in the running play, so a single-tag deploy cannot
see another role's defaults. This script copies all role defaults into a marked
block inside group_vars/all.yml (the special 'all' group, always loaded).

Run after editing any role defaults:

    python3 scripts/build-role-defaults.py

Only the marked block is regenerated; site config and vault_* values are preserved.
"""
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ROLES = os.path.join(ROOT, 'roles')
ALL_YML = os.path.join(ROOT, 'group_vars', 'all.yml')
BEGIN = '# >>> generated role defaults (ADR-63) — do not edit below this line >>>'
END   = '# <<< end generated role defaults <<<'

with open(ALL_YML, encoding='utf-8') as f:
    text = f.read()

# Keep everything before the generated block (site config / prechecks / vault / overrides)
if BEGIN in text:
    head = text[:text.index(BEGIN)].rstrip()
else:
    head = text.rstrip()

parts = [head, '', BEGIN,
         '# Source of truth: roles/<role>/defaults/main.yml',
         '# Regenerate with: python3 scripts/build-role-defaults.py', '']
count = 0
for role in sorted(os.listdir(ROLES)):
    p = os.path.join(ROLES, role, 'defaults', 'main.yml')
    if not os.path.isfile(p):
        continue
    with open(p, encoding='utf-8') as f:
        lines = f.read().split('\n')
    out = []
    for line in lines:
        if line.strip() == '---' or line.startswith('# Role-owned defaults') or \
           line.startswith('# as <name>_overrides'):
            continue
        out.append(line)
    while out and not out[0].strip(): out.pop(0)
    while out and not out[-1].strip(): out.pop()
    if out:
        parts += ['# ---- role: ' + role + ' ----'] + out + ['']
        count += 1
parts.append(END)

with open(ALL_YML, 'w', encoding='utf-8', newline='') as f:
    f.write('\n'.join(parts).rstrip() + '\n')
print('wrote', ALL_YML, 'with', count, 'role blocks')
