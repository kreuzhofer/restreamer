"""Loopback-only production relay and capture adapters for the experiment."""
import base64
import json
import os
import pathlib
import re
import select
import threading
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid

import benchmark


def free_address():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return '127.0.0.1:' + str(listener.getsockname()[1])


class Delivery:
    def __init__(self, output, relay, sinks, seconds, target_fault=False):
        self.output = output
        self.target_fault = target_fault
        self.relay = self.sinks = None
        self.logs = []
        self.http = None
        try:
            log = (output / 'sinks.log').open('wb')
            self.logs.append(log)
            self.sinks = subprocess.Popen([str(sinks), '-output', str(output / 'sinks'),
                                           '-seconds', str(min(14500, seconds + 90))] + (['-disconnect-after', '15s'] if target_fault else []),
                                          stdout=subprocess.PIPE, stderr=log)
            if not select.select([self.sinks.stdout], [], [], 10)[0]:
                raise benchmark.ScreenError('local RTMP capture listeners did not become ready')
            ready = json.loads(self.sinks.stdout.readline(65536))
            ingest, self.http = free_address(), free_address()
            config = {'listen': ingest, 'health_listen': self.http, 'application': 'live',
                      'stream_key': 'benchmark-input-key', 'queue_bytes': 16 * 1024 * 1024,
                      'dashboard_username': 'benchmark', 'dashboard_password': 'benchmark-local-password',
                      'targets': ready['targets']}
            path = output / 'isolated-relay.json'
            path.write_text(json.dumps(config))
            path.chmod(0o600)
            log = (output / 'relay.log').open('wb')
            self.logs.append(log)
            self.relay = subprocess.Popen([str(relay), '-config', str(path)], stdout=log, stderr=log)
            deadline = time.monotonic() + 10
            while True:
                try:
                    self.request('/api/stage')
                    break
                except (OSError, urllib.error.URLError):
                    if self.relay.poll() is not None or time.monotonic() > deadline:
                        raise benchmark.ScreenError('isolated relay did not become ready')
                    time.sleep(0.05)
            self.command('go_live')
            self.url = 'rtmp://' + ingest + '/live/benchmark-input-key'
        except BaseException:
            self.close()
            raise

    def request(self, path, data=None):
        auth = base64.b64encode(b'benchmark:benchmark-local-password').decode('ascii')
        headers = {'Authorization': 'Basic ' + auth, 'X-Restreamer-Control': '1',
                   'Content-Type': 'application/json'}
        request = urllib.request.Request('http://' + self.http + path,
                                         data=json.dumps(data).encode() if data is not None else None,
                                         headers=headers)
        with urllib.request.urlopen(request, timeout=3) as response:
            return json.load(response)

    def command(self, action):
        state = self.request('/api/stage')
        result = self.request('/api/stage/commands', {'id': uuid.uuid4().hex,
                              'server_id': state['server_id'], 'context': state['context'],
                              'action': action, 'confirmed': True, 'mode': 'real'})
        if result.get('state') not in ('completed', 'pending'):
            raise benchmark.ScreenError('isolated relay rejected the benchmark command')

    def finish(self):
        # Allow the final local writes to arrive, then stop via the public API.
        # Counts/markers must still prove that no frame was lost during cleanup.
        time.sleep(1)
        if self.relay.poll() is not None or self.sinks.poll() is not None:
            raise benchmark.ScreenError('a local delivery worker exited unexpectedly')
        self.status = self.request('/api/stage')
        self.command('stop_now')
        benchmark.stop_process(self.relay)
        benchmark.stop_process(self.sinks)
        summary_path = self.output / 'sinks' / 'summary.json'
        if not summary_path.exists():
            raise benchmark.ScreenError('capture helper did not flush its evidence')
        summary = json.loads(summary_path.read_text())
        expected = [2, 1] if self.target_fault else [1, 1]
        if len(summary) != 2 or any(item['connections'] != count or item['rejected'] or item['write_failures'] for item, count in zip(summary, expected)):
            raise benchmark.ScreenError('unexpected destination reconnect or capture failure')
        capture = self.output / 'sinks' / ('sink-2' if self.target_fault else 'sink-1') / 'connection-001.flv'
        os.link(capture, self.output / 'capture.flv')
        return summary

    def close(self):
        for process in (self.relay, self.sinks):
            if process:
                benchmark.stop_process(process)
        if self.sinks and self.sinks.stdout:
            self.sinks.stdout.close()
        for log in self.logs:
            log.close()


def timings(output, anchor, expected_frames, sync_markers=False, target_fault=False):
    counts, warmups, delivery_max, live_max = [], [], 0, 0
    av_errors = []
    command_latencies = []
    for number in ((2,) if target_fault else (1, 2)):
        capture = output / 'sinks' / ('sink-' + str(number)) / 'connection-001.flv'
        events_path = capture.with_suffix('.jsonl')
        count = warmup = 0
        previous_pts = None
        flash = False
        sync_pts = []
        log_path = output / ('sink-{}-decode.log'.format(number))
        with log_path.open('wb') as log:
            decoder = subprocess.Popen(['ffmpeg', '-v', 'error', '-nostdin', '-xerror',
                                        '-err_detect', 'explode', '-threads', '2', '-i', str(capture),
                                        '-filter_complex_threads', '1', '-filter_complex',
                                        '[0:v]split=2[a][b];[a]crop=176:8:0:0,format=gray[top];'
                                        '[b]crop=32:8:iw-32:ih-32,format=gray,pad=176:8:0:0:black[bottom];'
                                        '[top][bottom]vstack[out]', '-map', '[out]', '-fps_mode', 'passthrough',
                                        '-f', 'rawvideo', 'pipe:1'], stdout=subprocess.PIPE, stderr=log)
            timeout = threading.Timer(expected_frames / 25 * 2 + 60, decoder.kill)
            timeout.start()
            try:
                with events_path.open() as events, (output / 'selected-frames.jsonl').open() as selected:
                    while True:
                        pixels = decoder.stdout.read(176 * 16)
                        if not pixels:
                            break
                        if len(pixels) != 176 * 16:
                            raise benchmark.ScreenError('decoded source marker was truncated')
                        event_line = events.readline()
                        if not event_line:
                            raise benchmark.ScreenError('capture arrival evidence ended early')
                        event = json.loads(event_line)
                        frame = sum(1 << bit for bit in range(20) if pixels[3 * 176 + 16 + bit * 8 + 3] > 128)
                        if previous_pts is not None and event['pts_ms'] - previous_pts != 40:
                            raise benchmark.ScreenError('destination video timestamp cadence changed')
                        previous_pts = event['pts_ms']
                        if frame == (1 << 20) - 1:
                            if count:
                                raise benchmark.ScreenError('warm-up reappeared during measured playback')
                            warmup += 1
                            continue
                        intended_line = selected.readline()
                        if not intended_line:
                            raise benchmark.ScreenError('destination emitted extra measured frames')
                        intended = json.loads(intended_line)
                        source = round((pixels[3 * 176 + 3] - 19) / 74)
                        if frame != count or intended['frame'] != frame or intended['source'] != source:
                            raise benchmark.ScreenError('decoded frame/source marker disagrees with the output clock')
                        current_flash = pixels[11 * 176 + 3] > 200 and source in (0, 1)
                        if sync_markers and current_flash and not flash:
                            sync_pts.append(event['pts_ms'] / 1000)
                        flash = current_flash
                        arrival = event['arrival_ns'] / 1e9 - anchor
                        latency = arrival - intended['selected_at']
                        if latency < -0.01:
                            raise benchmark.ScreenError('delivery clocks or frame alignment are inconsistent')
                        command = intended.get('command')
                        if command and command['ready']:
                            if source != command['source']:
                                raise benchmark.ScreenError('ready command did not select the requested source')
                            command_latencies.append(arrival - command['at'])
                        delivery_max = max(delivery_max, latency)
                        if intended['ingress_at'] is not None:
                            live_max = max(live_max, arrival - intended['ingress_at'])
                        count += 1
                    if selected.readline() or events.readline() or count != expected_frames:
                        raise benchmark.ScreenError('destination lost measured output frames')
                if decoder.wait() != 0:
                    raise benchmark.ScreenError('captured destination video did not decode cleanly')
            finally:
                timeout.cancel()
                benchmark.stop_process(decoder)
                decoder.stdout.close()
        if log_path.stat().st_size:
            raise benchmark.ScreenError('captured destination video reported decoding errors')
        if sync_markers:
            av_errors.extend(audio_sync(capture, sync_pts, output / ('sink-{}-audio.log'.format(number)), expected_frames / 25))
        counts.append(count)
        warmups.append(warmup)
    result = {'delivered_video_frames': counts, 'warmup_frames_delivered': warmups,
            'maximum_delivery_latency_seconds': delivery_max,
            'maximum_ready_command_latency_seconds': max(command_latencies) if command_latencies else None,
            'ready_commands_checked': len(command_latencies),
            'maximum_live_relay_delay_seconds': live_max,
            'ingress_measurement': 'decoded_frame_arrival; compressed-input decode delay is not included'}

    if sync_markers:
        if not av_errors:
            raise benchmark.ScreenError('input flash/chirp markers were not observed')
        result['maximum_av_sync_error_seconds'] = max(av_errors)
        result['av_sync_events_checked'] = len(av_errors)
    if target_fault:
        result['recovery'] = recovered_destination(output, expected_frames)
    return result


def audio_sync(capture, expected, log_path, seconds):
    with log_path.open('wb') as log:
        process = subprocess.run(['ffmpeg', '-hide_banner', '-nostdin', '-nostats', '-v', 'info',
                                  '-xerror', '-copyts', '-i', str(capture), '-map', '0:a:0',
                                  '-af', 'pan=mono|c0=c1,silencedetect=noise=-20dB:d=0.05',
                                  '-f', 'null', '-'], stdout=subprocess.DEVNULL, stderr=log,
                                 timeout=seconds * 2 + 60)
    if process.returncode:
        raise benchmark.ScreenError('captured audio did not decode cleanly')
    pulses = []
    for line in log_path.read_text().splitlines():
        match = re.search(r'silence_end: ([0-9.]+)', line)
        if match:
            pulses.append(float(match.group(1)))
    index, errors = 0, []
    for timestamp in expected:
        while index < len(pulses) and pulses[index] < timestamp - 0.08:
            index += 1
        if index == len(pulses) or abs(pulses[index] - timestamp) > 0.08:
            raise benchmark.ScreenError('input flash/chirp A/V alignment exceeded 80 ms at {:.3f}s'.format(timestamp))
        errors.append(abs(pulses[index] - timestamp))
        index += 1
    return errors


def recovered_destination(output, expected_frames):
    directory = output / 'sinks' / 'sink-1'
    captures = sorted(directory.glob('connection-*.flv'))
    if len(captures) != 2:
        raise benchmark.ScreenError('injected destination failure did not recover exactly once')
    expected = [json.loads(line) for line in (output / 'selected-frames.jsonl').read_text().splitlines()]
    previous_frame, previous_arrival = -1, None
    gap = None
    for connection, capture in enumerate(captures):
        log_path = output / ('recovery-{}.log'.format(connection))
        with log_path.open('wb') as log, capture.with_suffix('.jsonl').open() as events:
            decoder = subprocess.Popen(['ffmpeg', '-v', 'error', '-nostdin', '-xerror',
                                        '-threads', '2', '-i', str(capture), '-map', '0:v:0',
                                        '-filter_threads', '1', '-vf', 'crop=176:8:0:0,format=gray',
                                        '-fps_mode', 'passthrough', '-f', 'rawvideo', 'pipe:1',
                                        '-map', '0:a:0', '-f', 'null', os.devnull],
                                       stdout=subprocess.PIPE, stderr=log)
            timer = threading.Timer(120, decoder.kill)
            timer.start()
            first = True
            try:
                while True:
                    pixels = decoder.stdout.read(176 * 8)
                    if not pixels:
                        break
                    event_line = events.readline()
                    if len(pixels) != 176 * 8 or not event_line:
                        raise benchmark.ScreenError('recovered destination capture is incomplete')
                    event = json.loads(event_line)
                    frame = sum(1 << bit for bit in range(20) if pixels[3 * 176 + 16 + bit * 8 + 3] > 128)
                    if frame == (1 << 20) - 1 and previous_frame == -1:
                        continue
                    if frame >= expected_frames or frame <= previous_frame:
                        raise benchmark.ScreenError('destination recovery replayed or invented frames')
                    if first and connection:
                        gap = (event['arrival_ns'] - previous_arrival) / 1e9
                    elif frame != previous_frame + 1:
                        raise benchmark.ScreenError('affected destination dropped frames outside its disconnect')
                    source = round((pixels[3 * 176 + 3] - 19) / 74)
                    if source != expected[frame]['source']:
                        raise benchmark.ScreenError('destination recovery selected stale source content')
                    previous_frame, previous_arrival = frame, event['arrival_ns']
                    first = False
                if events.readline() or decoder.wait() != 0:
                    raise benchmark.ScreenError('recovered destination media failed decoding')
            finally:
                timer.cancel()
                benchmark.stop_process(decoder)
                decoder.stdout.close()
        if log_path.stat().st_size:
            raise benchmark.ScreenError('recovered destination reported media errors')
    if previous_frame != expected_frames - 1 or gap is None or gap > 10:
        raise benchmark.ScreenError('affected destination did not recover within the probe deadline')
    return {'last_frame': previous_frame, 'gap_seconds': gap}
