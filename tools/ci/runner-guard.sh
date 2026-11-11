#!/usr/bin/env bash
# Install outside the checkout as ACTIONS_RUNNER_HOOK_JOB_STARTED.
# Diagnostic check only: maintainer approval, not this hook, gates fork workflows.
set -euo pipefail
python3 - <<'PY'
import json
import os
import sys


def reject():
    print('Refusing untrusted runner job: only this repository CI on main or the approved feature-branch push is allowed.', file=sys.stderr)
    raise SystemExit(1)


event = os.environ.get('GITHUB_EVENT_NAME')
ref = os.environ.get('GITHUB_REF')
if not ((ref == 'refs/heads/main' and event in ('push', 'workflow_dispatch'))
        or (ref == 'refs/heads/feature/nebius-show-package' and event == 'push')):
    reject()

expected = {
    'GITHUB_ACTIONS': 'true',
    'GITHUB_REPOSITORY': 'kreuzhofer/restreamer',
    'GITHUB_WORKFLOW_REF': f'kreuzhofer/restreamer/.github/workflows/ci.yml@{ref}',
}
if any(os.environ.get(key) != value for key, value in expected.items()):
    reject()
try:
    with open(os.environ['GITHUB_EVENT_PATH']) as source:
        payload = json.load(source)
    repository = payload['repository']
    if repository['full_name'] != 'kreuzhofer/restreamer' or repository['fork'] is not False:
        reject()
    if 'pull_request' in payload:
        reject()
    allowed_refs = (ref,) if event == 'push' else ('main', 'refs/heads/main')
    if payload['ref'] not in allowed_refs:
        reject()
except (OSError, KeyError, TypeError, ValueError):
    reject()
print('Trusted CI job accepted.')
PY
