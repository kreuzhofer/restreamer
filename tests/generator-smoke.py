"""Exercise generated media in the packaged server using local dummy credentials."""
import base64
import json
import os
import sys
from pathlib import Path
import time
import urllib.request

base = os.environ.get('GENERATOR_URL', 'http://127.0.0.1:18792')
auth = base64.b64encode((os.environ.get('GENERATOR_USER', 'stage-test') + ':' + os.environ.get('GENERATOR_PASSWORD', 'stage-test')).encode()).decode()
def request(path, method='GET', body=None, raw=False):
    data = None if body is None else json.dumps(body).encode()
    headers = {'Authorization': 'Basic ' + auth, 'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}
    with urllib.request.urlopen(urllib.request.Request(base + path, data=data, headers=headers, method=method), timeout=30) as response:
        data = response.read()
        return data if raw else (json.loads(data) if data else None)

def wait_for(path, predicate, timeout=120):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        value = request(path)
        if predicate(value):
            return value
        time.sleep(.2)
    raise AssertionError('Timed out waiting for ' + path)

def stage_command(action, **options):
    stage = request('/api/stage')
    body = dict(id='generator-smoke-' + str(time.time_ns()), server_id=stage['server_id'], context=stage['context'], action=action, confirmed=True, **options)
    result = request('/api/stage/commands', 'POST', body)
    assert result['state'] in ('completed', 'pending'), result
    return result

if '--verify-restart' in sys.argv:
    saved = json.loads(Path(os.environ['GENERATOR_SMOKE_STATE']).read_text())
    restored = request('/api/generator/jobs/' + saved['job'])
    assert restored['state'] == 'ready' and restored['media_revision'] == saved['revision'], restored
    assert restored['design_snapshot']['scenes'][0]['text'] == 'Welcome\nGrüße ΩЖ', restored
    print('Generated revision and captured inputs survived restart')
    sys.exit(0)

draft = request('/api/generator/designs', 'POST', {'name': 'Generated smoke title', 'stage': 'prestream'})
draft['scenes'][0]['text'] = 'Welcome\nGrüße ΩЖ'
draft['scenes'][0]['duration_seconds'] = 1.2
draft = request('/api/generator/designs/' + draft['id'], 'PUT', draft)
job = request('/api/generator/jobs', 'POST', {'design_id': draft['id'], 'version': draft['version']})
draft['scenes'][0]['text'] = 'Later draft must not change captured output'
draft = request('/api/generator/designs/' + draft['id'], 'PUT', draft)
job = wait_for('/api/generator/jobs/' + job['id'], lambda item: item['state'] in ('ready', 'failed', 'cancelled', 'interrupted'))
assert job['state'] == 'ready', job
revision = job['media_revision']
output = Path(os.environ.get('GENERATOR_SMOKE_OUTPUT', '/tmp/restreamer-generator-smoke.mp4'))
output.write_bytes(request('/api/library/revisions/' + revision + '/preview', raw=True))
status = request('/status')
assert status['stage']['stage'] == 'OFF' and not status['forwarding'], status
request('/api/stage-media', 'PUT', {'prestream': revision, 'ending': revision, 'shortcuts': []})
stage_command('prestream', mode='preview_only', revision=revision)
state = request('/api/stage')
assert state['stage'] == 'PRESTREAM' and state['mode'] == 'preview_only' and state['media']['revision'] == revision, state
stage_command('end_stream', revision=revision)
wait_for('/api/stage', lambda state: state['stage'] == 'OFF', timeout=15)
status = request('/status')
assert not status['forwarding'] and all(target['attempts'] == 0 for target in status['outputs']), status
result = {'job': job['id'], 'revision': revision, 'preview': str(output), 'stage': status['stage']['stage']}
if os.environ.get('GENERATOR_SMOKE_STATE'):
    Path(os.environ['GENERATOR_SMOKE_STATE']).write_text(json.dumps(result))
print(json.dumps(result))
