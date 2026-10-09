"""Exercise generated media in the packaged server using local dummy credentials."""
import base64
import json
import os
import sys
from pathlib import Path
import time
import urllib.request
import uuid

base = os.environ.get('GENERATOR_URL', 'http://127.0.0.1:18792')
auth = base64.b64encode((os.environ.get('GENERATOR_USER', 'stage-test') + ':' + os.environ.get('GENERATOR_PASSWORD', 'stage-test')).encode()).decode()
def request(path, method='GET', body=None, raw=False, content_type='application/json'):
    data = body if isinstance(body, bytes) else (None if body is None else json.dumps(body).encode())
    headers = {'Authorization': 'Basic ' + auth, 'Content-Type': content_type, 'X-Restreamer-Control': '1'}
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
    video = request('/api/generator/jobs/' + saved['video_job'])
    assert video['state'] == 'ready' and video['media_revision'] == saved['video_revision'], video
    assert video['design_snapshot']['scenes'][0]['video']['asset'] == saved['video_asset'], video
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

# Exercise uploaded H.264/AAC through the packaged trim/repeat and PCM pipeline.
source = Path(os.environ['GENERATOR_SMOKE_SOURCE']).read_bytes()
boundary = 'restreamer-' + uuid.uuid4().hex
multipart = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="smoke.mp4"\r\nContent-Type: video/mp4\r\n\r\n').encode() + source + ('\r\n--' + boundary + '--\r\n').encode()
asset = request('/api/generator/assets?kind=video', 'POST', multipart, content_type='multipart/form-data; boundary=' + boundary)
assert asset['kind'] == 'video' and asset['revisions'][0]['has_audio'], asset
asset_ref = {'id': asset['id'], 'revision': asset['revision']}
video_draft = request('/api/generator/designs', 'POST', {'name': 'Video trim and repeat smoke', 'stage': 'prestream'})
video_draft['scenes'][0].update(layout='media', media_kind='video', duration_seconds=1.2, video=dict(asset=asset_ref, trim_start_seconds=.2, trim_end_seconds=.8, repeat=True, audio_enabled=True, audio_volume_percent=50))
video_draft = request('/api/generator/designs/' + video_draft['id'], 'PUT', video_draft)
video_job = request('/api/generator/jobs', 'POST', {'design_id': video_draft['id'], 'version': video_draft['version']})
video_job = wait_for('/api/generator/jobs/' + video_job['id'], lambda item: item['state'] in ('ready', 'failed', 'cancelled', 'interrupted'))
assert video_job['state'] == 'ready', video_job
video_output = output.with_name(output.stem + '-video.mp4')
video_output.write_bytes(request('/api/library/revisions/' + video_job['media_revision'] + '/preview', raw=True))

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
result = {'job': job['id'], 'revision': revision, 'preview': str(output), 'stage': status['stage']['stage'], 'video_job': video_job['id'], 'video_revision': video_job['media_revision'], 'video_asset': asset_ref, 'video_preview': str(video_output)}
if os.environ.get('GENERATOR_SMOKE_STATE'):
    Path(os.environ['GENERATOR_SMOKE_STATE']).write_text(json.dumps(result))
print(json.dumps(result))
