"""Exercise generated media in the packaged server using local dummy credentials."""
import base64
import array
import io
import json
import math
import os
import struct
import sys
from pathlib import Path
import time
import urllib.request
import uuid
import wave
import zlib

base = os.environ.get('GENERATOR_URL', 'http://127.0.0.1:18792')
auth = base64.b64encode((os.environ.get('GENERATOR_USER', 'stage-test') + ':' + os.environ.get('GENERATOR_PASSWORD', 'stage-test')).encode()).decode()
def request(path, method='GET', body=None, raw=False, content_type='application/json'):
    data = body if isinstance(body, bytes) else (None if body is None else json.dumps(body).encode())
    headers = {'Authorization': 'Basic ' + auth, 'Content-Type': content_type, 'X-Restreamer-Control': '1'}
    with urllib.request.urlopen(urllib.request.Request(base + path, data=data, headers=headers, method=method), timeout=30) as response:
        data = response.read()
        return data if raw else (json.loads(data) if data else None)

def upload_asset(kind, name, content_type, data):
    boundary = 'restreamer-' + uuid.uuid4().hex
    multipart = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="' + name + '"\r\nContent-Type: ' + content_type + '\r\n\r\n').encode() + data + ('\r\n--' + boundary + '--\r\n').encode()
    return request('/api/generator/assets?kind=' + kind, 'POST', multipart, content_type='multipart/form-data; boundary=' + boundary)

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

if '--verify-video' in sys.argv:
    saved = json.loads(Path(os.environ['GENERATOR_SMOKE_STATE']).read_text())
    probe = json.loads(Path(sys.argv[sys.argv.index('--verify-video') + 1]).read_text())
    assert len(probe['streams']) == 1, probe
    assert int(probe['streams'][0]['nb_read_frames']) == saved['video_expected_frames'], (probe, saved['video_expected_frames'])
    print('Decoded crossfade frame count:', saved['video_expected_frames'])
    sys.exit(0)

if '--verify-ending' in sys.argv:
    saved = json.loads(Path(os.environ['GENERATOR_SMOKE_STATE']).read_text())
    prefix = Path(sys.argv[sys.argv.index('--verify-ending') + 1])
    frames = prefix.with_suffix('.rgb').read_bytes()
    assert len(frames) == saved['ending_frames'] * 16 * 16 * 3, len(frames)
    last_frame = frames[-16 * 16 * 3:]
    assert max(last_frame) == 0, ('Ending must finish on a black frame', min(last_frame), max(last_frame), sorted(set(last_frame)))
    samples = array.array('h', prefix.with_suffix('.pcm').read_bytes())
    end = round(saved['ending_duration'] * saved['sample_rate'])
    tail = samples[end - saved['sample_rate'] // 100:end]
    assert len(tail) == saved['sample_rate'] // 100, 'Ending audio is too short'
    assert max(abs(value) for value in tail) <= 4, 'Presented ending tail must be silent'
    print('Ending frame count, final black frame, and presented audio silence passed')
    sys.exit(0)

if '--verify-audio' in sys.argv:
    saved = json.loads(Path(os.environ['GENERATOR_SMOKE_STATE']).read_text())
    samples = array.array('h', Path(sys.argv[sys.argv.index('--verify-audio') + 1]).read_bytes())
    rate = saved['sample_rate']
    window = samples[rate // 3:rate // 3 + rate // 5]
    assert len(window) == rate // 5, 'Generated audio is too short'
    for frequency in (440, 880):
        real = sum(value * math.cos(2 * math.pi * frequency * i / rate) for i, value in enumerate(window))
        imaginary = sum(value * math.sin(2 * math.pi * frequency * i / rate) for i, value in enumerate(window))
        amplitude = 2 * math.hypot(real, imaginary) / len(window)
        assert amplitude > 100, (frequency, amplitude)
        print('Decoded tone', frequency, 'Hz amplitude:', round(amplitude))
    sys.exit(0)

if '--verify-restart' in sys.argv:
    saved = json.loads(Path(os.environ['GENERATOR_SMOKE_STATE']).read_text())
    restored = request('/api/generator/jobs/' + saved['job'])
    assert restored['state'] == 'ready' and restored['media_revision'] == saved['revision'], restored
    assert restored['design_snapshot']['scenes'][0]['text'] == 'Welcome\nGrüße ΩЖ', restored
    video = request('/api/generator/jobs/' + saved['video_job'])
    assert video['state'] == 'ready' and video['media_revision'] == saved['video_revision'], video
    assert video['design_snapshot']['scenes'][0]['video']['asset'] == saved['video_asset'], video
    assert video['design_snapshot']['soundtrack']['asset'] == saved['music_asset'], video
    assert video['design_snapshot']['theme'] == saved['theme'], video
    assert video['theme_snapshot']['style']['effect'] == 'pixel-trail', video
    assert video['theme_snapshot']['style']['logo'] == saved['logo_asset'], video
    assert video['design_snapshot']['scenes'][0]['transition'] == {'kind': 'crossfade', 'duration_seconds': .2}, video
    assert video['design_snapshot']['loop_transition'] == {'kind': 'crossfade', 'duration_seconds': .2}, video
    ending = request('/api/generator/jobs/' + saved['ending_job'])
    assert ending['state'] == 'ready' and ending['media_revision'] == saved['ending_revision'], ending
    assert ending['duration'] == saved['ending_duration'], ending
    brb = request('/api/dashboard')['brb_assets']
    assert brb['generation'] == saved['brb_generation'] and brb['theme'] == saved['brb_theme'], brb
    assert request('/api/brb/theme/candidate') is None
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
asset = upload_asset('video', 'smoke.mp4', 'video/mp4', source)
assert asset['kind'] == 'video' and asset['revisions'][0]['has_audio'], asset
asset_ref = {'id': asset['id'], 'revision': asset['revision']}
music_bytes = io.BytesIO()
with wave.open(music_bytes, 'wb') as wav:
    wav.setparams((1, 2, 48000, 0, 'NONE', 'not compressed'))
    wav.writeframes(b''.join(struct.pack('<h', round(5000 * math.sin(2 * math.pi * 440 * i / 48000))) for i in range(14400)))
music = upload_asset('audio', 'smoke.wav', 'audio/wav', music_bytes.getvalue())
music_ref = {'id': music['id'], 'revision': music['revision']}
def png_chunk(kind, data):
    return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data))
logo_png = b'\x89PNG\r\n\x1a\n' + png_chunk(b'IHDR', struct.pack('>IIBBBBB', 16, 8, 8, 2, 0, 0, 0)) + png_chunk(b'IDAT', zlib.compress((b'\x00' + b'\xff\xdd\x00' * 16) * 8)) + png_chunk(b'IEND', b'')
logo = upload_asset('image', 'smoke.png', 'image/png', logo_png)
logo_ref = {'id': logo['id'], 'revision': logo['revision']}
theme = request('/api/generator/themes', 'POST', {'name': 'Container smoke theme', 'base': {'id': 'retro', 'revision': 2}})
theme['style']['logo'] = logo_ref
theme = request('/api/generator/themes/' + theme['id'], 'PUT', theme)
theme_ref = {'id': theme['id'], 'revision': theme['revision']}
video_draft = request('/api/generator/designs', 'POST', {'name': 'Video trim and repeat smoke', 'stage': 'prestream', 'theme': theme_ref})
video_draft['scenes'][0].update(layout='media', media_kind='video', duration_seconds=1.2, content_region={'width_percent': 60, 'height_percent': 60}, video=dict(asset=asset_ref, trim_start_seconds=.2, trim_end_seconds=.8, repeat=True, audio_enabled=True, audio_volume_percent=50))
video_draft['scenes'][0]['transition'] = {'kind': 'crossfade', 'duration_seconds': .2}
video_draft['scenes'].append(dict(id='smoke-next', layout='title', text='Next scene', duration_seconds=.6))
video_draft['soundtrack'] = dict(asset=music_ref, mode='repeat', volume_percent=30, fade_in_seconds=.1, fade_out_seconds=.1)
video_draft['loop_transition'] = {'kind': 'crossfade', 'duration_seconds': .2}
video_draft = request('/api/generator/designs/' + video_draft['id'], 'PUT', video_draft)
video_job = request('/api/generator/jobs', 'POST', {'design_id': video_draft['id'], 'version': video_draft['version']})
# A shared theme edit must not change the captured rendering or retained logo.
theme['style']['effect'] = 'none'
theme['style'].pop('logo')
request('/api/generator/themes/' + theme['id'], 'PUT', theme)
video_job = wait_for('/api/generator/jobs/' + video_job['id'], lambda item: item['state'] in ('ready', 'failed', 'cancelled', 'interrupted'))
assert video_job['state'] == 'ready', video_job
assert 0 < video_job['mix_gain'] <= 1, video_job
assert video_job['theme_snapshot']['style']['effect'] == 'pixel-trail', video_job
assert video_job['theme_snapshot']['style']['logo'] == logo_ref, video_job
video_expected_frames = 2 * round(1.4 * video_job['profile']['fps'])
assert math.isclose(video_job['duration'], video_expected_frames / 2 / video_job['profile']['fps'], abs_tol=1e-9), video_job
video_output = output.with_name(output.stem + '-video.mp4')
video_output.write_bytes(request('/api/library/revisions/' + video_job['media_revision'] + '/preview?passes=2', raw=True))

# An ending uses the same captured scenes/music, with its default finite fade.
ending_draft = request('/api/generator/designs', 'POST', {'name': 'Finite ending smoke', 'stage': 'ending', 'theme': theme_ref})
ending_draft.update(scenes=video_draft['scenes'], soundtrack=video_draft['soundtrack'])
ending_draft = request('/api/generator/designs/' + ending_draft['id'], 'PUT', ending_draft)
ending_job = request('/api/generator/jobs', 'POST', {'design_id': ending_draft['id'], 'version': ending_draft['version']})
ending_job = wait_for('/api/generator/jobs/' + ending_job['id'], lambda item: item['state'] in ('ready', 'failed', 'cancelled', 'interrupted'))
assert ending_job['state'] == 'ready' and math.isclose(ending_job['duration'], 1.6, abs_tol=1e-9), ending_job
ending_output = output.with_name(output.stem + '-ending.mp4')
ending_output.write_bytes(request('/api/library/revisions/' + ending_job['media_revision'] + '/preview', raw=True))

# BRB preparation retains the current media until explicit activation, and pins
# the same immutable theme revision even after the shared theme was edited.
brb_before = request('/api/dashboard')['brb_assets']
brb_candidate = request('/api/brb/theme/prepare', 'POST', {'theme': theme_ref, 'base_generation': brb_before['generation']})
assert request('/api/dashboard')['brb_assets'] == brb_before
assert brb_candidate['settings']['theme'] == video_job['theme_snapshot'], brb_candidate
brb_output = output.with_name(output.stem + '-brb.mp4')
brb_output.write_bytes(request('/api/brb/theme/candidate/preview?id=' + brb_candidate['id'], raw=True))
request('/api/brb/theme/activate', 'POST', {'id': brb_candidate['id'], 'base_generation': brb_before['generation']})
brb_current = request('/api/dashboard')['brb_assets']
assert brb_current['generation'] == brb_candidate['id'], brb_current
assert request('/api/brb/theme/candidate') is None

status = request('/status')
assert status['stage']['stage'] == 'OFF' and not status['forwarding'], status
request('/api/stage-media', 'PUT', {'prestream': revision, 'ending': ending_job['media_revision'], 'shortcuts': []})
stage_command('prestream', mode='preview_only', revision=revision)
state = request('/api/stage')
assert state['stage'] == 'PRESTREAM' and state['mode'] == 'preview_only' and state['media']['revision'] == revision, state
stage_command('end_stream', revision=ending_job['media_revision'])
wait_for('/api/stage', lambda state: state['stage'] == 'OFF', timeout=15)
status = request('/status')
assert not status['forwarding'] and all(target['attempts'] == 0 for target in status['outputs']), status
result = {'job': job['id'], 'revision': revision, 'preview': str(output), 'stage': status['stage']['stage'], 'video_job': video_job['id'], 'video_revision': video_job['media_revision'], 'video_asset': asset_ref, 'music_asset': music_ref, 'logo_asset': logo_ref, 'theme': theme_ref, 'sample_rate': video_job['profile']['sample_rate'], 'video_expected_frames': video_expected_frames, 'video_preview': str(video_output)}
result.update(brb_generation=brb_current['generation'], brb_theme=brb_current['theme'])
result.update(ending_job=ending_job['id'], ending_revision=ending_job['media_revision'], ending_duration=ending_job['duration'], ending_frames=round(ending_job['duration'] * ending_job['profile']['fps']))
if os.environ.get('GENERATOR_SMOKE_STATE'):
    Path(os.environ['GENERATOR_SMOKE_STATE']).write_text(json.dumps(result))
print(json.dumps(result))
