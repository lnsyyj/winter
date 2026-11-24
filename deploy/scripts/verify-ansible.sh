#!/usr/bin/env sh
# Static verification of the deploy/ tree. Runs without ansible-core.
# Usage: sh deploy/scripts/verify-ansible.sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

if ! command -v python3 >/dev/null 2>&1; then
    echo "python3 not found; install it to run the structural checks" >&2
    exit 3
fi

python3 - <<'PY'
import pathlib, re, sys

root = pathlib.Path('deploy')
errors = []
files = sorted(root.rglob('*.yml')) + sorted(root.rglob('*.yaml'))

# 1. YAML structure
for f in files:
    try:
        import yaml
        yaml.safe_load(f.read_text(encoding='utf-8'))
    except Exception as exc:
        errors.append(f'YAML {f}: {str(exc).splitlines()[0]}')

# 2. handler / notify consistency
handlers, notify = set(), set()
for f in files:
    import yaml
    data = yaml.safe_load(f.read_text(encoding='utf-8')) or []
    if not isinstance(data, list):
        continue
    for task in data:
        if not isinstance(task, dict):
            continue
        if 'handlers' in str(f) and 'name' in task:
            handlers.add(str(task['name']))
        if 'notify' in task:
            ref = task['notify']
            for name in (ref if isinstance(ref, list) else [ref]):
                if '{{' not in str(name):
                    notify.add(str(name))
unmatched = sorted(notify - handlers)
if unmatched:
    errors.append(f'unmatched notify: {unmatched}')

# 3. English-only rule: Han characters are allowed only inside real doc paths
for f in root.rglob('*'):
    if not f.is_file() or f.suffix not in {'.yml', '.yaml', '.sh', '.md', '.j2', '.ini', '.cfg'}:
        continue
    text = f.read_text(encoding='utf-8', errors='ignore')
    without_doc_paths = re.sub(r'docs/[^\s\)\]\"`]*', '', text)
    han = sorted(set(re.findall(r'[\u4e00-\u9fff]', without_doc_paths)))
    if han:
        errors.append(f'non-English text in {f}: {"".join(han)}')

# 4. Balanced Jinja expressions in templates
for f in root.rglob('*.j2'):
    text = f.read_text(encoding='utf-8', errors='ignore')
    if text.count('{{') != text.count('}}'):
        errors.append(f'unbalanced Jinja expression in {f}')

# 5. Markdown links inside deploy/ (doc paths are relative to the file)
for f in root.rglob('*.md'):
    for link in re.findall(r'\[[^\]]*\]\(([^)]+)\)', f.read_text(encoding='utf-8')):
        if link.startswith(('http://', 'https://', 'mailto:', '#')):
            continue
        target = link.split('#')[0]
        if target and not (f.parent / target).resolve().exists():
            errors.append(f'broken link {f} -> {link}')

print(f'YAML files parsed: {len(files)}')
if errors:
    print(f'FAILED: {len(errors)} issue(s)')
    for line in errors:
        print(' -', line)
    raise SystemExit(1)
print('All static checks passed')
PY

for f in deploy/deploy.sh deploy/scripts/*.sh; do
    sh -n "$f" || exit 1
done
echo "Shell syntax passed"

# Optional full syntax check when ansible-core is available on the control machine
if command -v ansible-playbook >/dev/null 2>&1; then
    # ansible.cfg (roles_path, etc.) lives in deploy/; use it explicitly because this script runs
    # from the repo root. scale-out-member.yml is a task fragment, not a playbook — skip it.
    ANSIBLE_CONFIG=deploy/ansible.cfg ansible-playbook --syntax-check \
        -i deploy/inventory/all-in-one deploy/site.yml >/dev/null || exit 1
    echo "ansible-playbook --syntax-check passed"
else
    echo "ansible-core not installed locally; skipped --syntax-check (run it on the control machine)"
fi
