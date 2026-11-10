#!/usr/bin/env python3
"""Experimental clocked playout. Not imported by the production application."""
import argparse
import array
import collections
import json
import math
import os
import pathlib
import queue
import shutil
import signal
import subprocess
import sys
import threading
import time
from types import SimpleNamespace

import benchmark
import delivery

WIDTH, HEIGHT, FPS, RATE = 1920, 1080, 25, 48000
VIDEO_BYTES = WIDTH * HEIGHT * 3 // 2
AUDIO_BYTES = RATE // FPS * 2 * 2
SILENCE = bytes(AUDIO_BYTES)
EMERGENCY = bytes([16]) * (WIDTH * HEIGHT) + bytes([128]) * (WIDTH * HEIGHT // 2)


def put_until_stopped(target, value, stop):
    while not stop.is_set():
        try:
            target.put(value, timeout=0.2)
            return
        except queue.Full:
            continue


class Source:
    def __init__(self, path, output, name, buffered):
        self.frames = collections.deque()
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.primed = False
        self.buffered = buffered
        self.dropped = 0
        self.failed = None
        self.origin = None
        self.paused = False
        self.video_queue = queue.Queue(4)
        self.audio_queue = queue.Queue(4)
        audio_read, audio_write = os.pipe()
        command = ['ffmpeg', '-hide_banner', '-nostdin', '-v', 'error',
                   '-protocol_whitelist', 'file,pipe', '-threads', '2', '-re', '-stream_loop', '-1',
                   '-i', str(path), '-filter_threads', '1', '-map', '0:v:0',
                   '-vf', 'scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2,fps=25,format=yuv420p',
                   '-c:v', 'rawvideo', '-threads', '1', '-f', 'rawvideo', 'pipe:1',
                   '-map', '0:a:0', '-af', 'aresample=48000:async=1000:min_hard_comp=0.001:first_pts=0',
                   '-ac', '2', '-f', 's16le', 'pipe:' + str(audio_write)]
        self.log = (output / (name + '-decoder.log')).open('wb')
        try:
            self.process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=self.log,
                                            stdin=subprocess.DEVNULL, pass_fds=(audio_write,))
        finally:
            os.close(audio_write)
        self.audio = os.fdopen(audio_read, 'rb')
        self.threads = [threading.Thread(target=self.read, args=(self.process.stdout, VIDEO_BYTES, self.video_queue), daemon=True),
                        threading.Thread(target=self.read, args=(self.audio, AUDIO_BYTES, self.audio_queue), daemon=True),
                        threading.Thread(target=self.pair, daemon=True)]
        for thread in self.threads:
            thread.start()

    def read(self, handle, size, target):
        try:
            while not self.stop.is_set():
                data = handle.read(size)
                if len(data) != size:
                    if not self.stop.is_set():
                        self.failed = 'source decoder ended unexpectedly'
                    return
                put_until_stopped(target, (time.monotonic(), data), self.stop)
        except (OSError, ValueError):
            if not self.stop.is_set():
                self.failed = 'source decoder pipe failed'

    def pair(self):
        sequence = 0
        while not self.stop.is_set():
            try:
                video_time, video = self.video_queue.get(timeout=0.2)
            except queue.Empty:
                continue
            audio_item = None
            while audio_item is None and not self.stop.is_set():
                try:
                    audio_item = self.audio_queue.get(timeout=0.2)
                except queue.Empty:
                    continue
            if audio_item is None:
                return
            phase = (video_time - self.origin) % 60 if self.origin else 0
            audio_available = not (self.buffered and 40 <= phase < 44)
            frame = (video_time, sequence, video, audio_item[1], audio_available)
            sequence += 1
            with self.lock:
                limit = 130 if self.buffered else 50
                if len(self.frames) == limit:
                    self.frames.popleft()
                    self.dropped += 1
                self.frames.append(frame)

    def next(self, now):
        with self.lock:
            if not self.frames:
                self.primed = False
                return None
            if not self.primed:
                if now - self.frames[0][0] < (4 if self.buffered else 0.5):
                    return None
                self.primed = True
            return self.frames.popleft()

    def pause(self, paused):
        if paused != self.paused and self.process.poll() is None:
            os.kill(self.process.pid, signal.SIGSTOP if paused else signal.SIGCONT)
            self.paused = paused

    def close(self):
        self.stop.set()
        # Disposable input decoders have no delivery tail to drain. Closing a
        # blocked raw pipe gracefully can deadlock; terminate this owned worker.
        if self.process.poll() is None:
            self.process.kill()
        self.process.wait(timeout=2)
        for thread in self.threads:
            thread.join(timeout=2)
        self.process.stdout.close()
        self.audio.close()
        self.log.close()


def marked_video(video, source, frame):
    result = bytearray(video)
    cells = [32 + source * 64] * 2 + [235 if frame & (1 << bit) else 16 for bit in range(20)]
    for row in range(8):
        base = row * WIDTH
        for column, value in enumerate(cells):
            result[base + column * 8:base + (column + 1) * 8] = bytes([value]) * 8
    return result


def marked_audio(audio, frame):
    # The right channel is reserved for a 10 ms sync chirp every second.
    # The left channel retains selected source content (or silence on loss).
    samples = array.array('h', audio)
    if sys.byteorder != 'little':
        samples.byteswap()
    pulse = frame % FPS == 0
    for index in range(RATE // FPS):
        samples[index * 2 + 1] = int(24000 * math.sin(2 * math.pi * 2000 * index / RATE)) if pulse and index < 480 else 0
    if sys.byteorder != 'little':
        samples.byteswap()
    return samples.tobytes()


def writer(handle, frames, stop, errors):
    try:
        while not stop.is_set():
            try:
                data = frames.get(timeout=0.2)
            except queue.Empty:
                continue
            if data is None:
                return
            handle.write(data)
            handle.flush()
    except (OSError, ValueError):
        errors.append('encoder input pipe failed')
    finally:
        handle.close()


def run(args):
    for path in (args.live, args.brb):
        metadata = benchmark.probe('ffprobe', path)
        if not all(any(stream.get('codec_type') == kind for stream in metadata['streams']) for kind in ('video', 'audio')):
            raise benchmark.ScreenError('both sources must contain video and audio')
    required = args.seconds * 8160000 / 8 * 2 * 1.3 + 1024 ** 3
    if shutil.disk_usage(args.output.parent).free < required:
        raise benchmark.ScreenError('insufficient space for bounded captures and reserve')
    args.output.mkdir(mode=0o700)
    report = {'schema_version': 1, 'scope': 'experimental_clocked_playout', 'status': 'running',
              'rollout_qualified': False, 'host': benchmark.host_info(), 'seconds': args.seconds,
              'input_sha256': {'live': benchmark.digest(args.live), 'brb': benchmark.digest(args.brb)},
              'source_sha256': {name: benchmark.digest(pathlib.Path(__file__).with_name(name))
                                for name in ('playout.py', 'benchmark.py', 'delivery.py')},
              'samples': [], 'sources': {}, 'commands': [], 'maximum_clock_lag_seconds': 0,
              'unmeasured_gates': ['representative_media', 'local_rtmp_delivery', 'output_arrival_latency',
                                   'av_content_sync', 'encoder_recovery', 'target_isolation',
                                   'rendering_contention', 'stop_and_ending_controls']}
    live = brb = encoder = delivered = None
    workers = []
    stop = threading.Event()
    errors = []
    code = 1
    try:
        if args.relay:
            delivered = delivery.Delivery(args.output, args.relay, args.sinks, args.seconds, args.target_fault)
        live = Source(args.live, args.output, 'live', True)
        brb = Source(args.brb, args.output, 'brb', False)
        ready_deadline = time.monotonic() + 10
        while not live.frames or not brb.frames or time.monotonic() - brb.frames[0][0] < 0.5:
            if live.failed or brb.failed or time.monotonic() >= ready_deadline:
                raise benchmark.ScreenError('source preparation did not become ready')
            time.sleep(0.02)
        audio_read, audio_write = os.pipe()
        command = ['ffmpeg', '-hide_banner', '-nostdin', '-v', 'warning', '-n',
                   '-thread_queue_size', '2', '-probesize', '32', '-analyzeduration', '0',
                   '-f', 'rawvideo', '-pixel_format', 'yuv420p', '-video_size', '1920x1080',
                   '-framerate', '25', '-i', 'pipe:0', '-thread_queue_size', '2',
                   '-probesize', '32', '-analyzeduration', '0', '-f', 's16le', '-ar', '48000', '-ac', '2',
                   '-i', 'pipe:' + str(audio_read), '-map', '0:v:0', '-map', '1:a:0',
                   '-t', str(args.seconds + (4 if delivered else 0)), '-c:v', 'libx264', '-threads', '3', '-preset', 'veryfast',
                   '-tune', 'zerolatency', '-pix_fmt', 'yuv420p', '-b:v', '8000k', '-minrate', '8000k',
                   '-maxrate', '8000k', '-bufsize', '16000k', '-g', '50', '-keyint_min', '50',
                   '-sc_threshold', '0', '-x264-params', 'nal-hrd=cbr:force-cfr=1',
                   '-c:a', 'aac', '-b:a', '160k', '-ar', '48000', '-ac', '2',
                   '-f', 'flv', delivered.url if delivered else str(args.output / 'capture.flv')]
        report['encoder_command'] = command
        with (args.output / 'encoder.log').open('wb') as log:
            try:
                encoder = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.DEVNULL,
                                           stderr=log, pass_fds=(audio_read,))
            finally:
                os.close(audio_read)
            audio_handle = os.fdopen(audio_write, 'wb')
            video_queue, audio_queue = queue.Queue(2), queue.Queue(2)
            workers = [threading.Thread(target=writer, args=(encoder.stdin, video_queue, stop, errors), daemon=True),
                       threading.Thread(target=writer, args=(audio_handle, audio_queue, stop, errors), daemon=True)]
            for worker in workers:
                worker.start()
            warm_brb = None
            if delivered:
                # Existing passthrough outputs connect at a keyframe. Warm up
                # explicitly; none of these frames belongs to the measured run.
                warm_start = time.monotonic()
                for warm_frame in range(100):
                    time.sleep(max(0, warm_start + warm_frame / FPS - time.monotonic()))
                    prepared = brb.next(time.monotonic())
                    if prepared:
                        warm_brb = prepared
                        warm_video, warm_audio = prepared[2], prepared[3]
                    elif warm_frame == 0:
                        raise benchmark.ScreenError('prepared source was not ready for warm-up')
                    video_queue.put(marked_video(warm_video, 1, (1 << 20) - 1), timeout=0.2)
                    audio_queue.put(warm_audio if args.sync_markers else marked_audio(warm_audio, warm_frame), timeout=0.2)
                with live.lock:
                    live.frames.clear()
                    live.primed = False
                report['warmup_frames_selected'] = 100
            start = time.monotonic()
            clock_anchor = time.time_ns() / 1e9 - time.monotonic()
            report['clock_anchor_unix_seconds'] = clock_anchor
            live.origin = brb.origin = start
            last_live, last_brb = None, warm_brb
            last_good = None
            previous_request = 0
            with (args.output / 'selected-frames.jsonl').open('w') as frames:
                for index in range(args.seconds * FPS):
                    deadline = start + index / FPS
                    time.sleep(max(0, deadline - time.monotonic()))
                    now = time.monotonic()
                    phase = (index / FPS) % 60
                    report['maximum_clock_lag_seconds'] = max(report['maximum_clock_lag_seconds'], now - deadline)
                    if errors or live.failed or brb.failed or encoder.poll() is not None:
                        raise benchmark.ScreenError('media worker failed; inspect the decoder/encoder logs')
                    live.pause(20 <= phase < 28 or 50 <= phase < 58)
                    candidate = live.next(now)
                    prepared = brb.next(now)
                    if prepared:
                        last_brb = prepared
                    if candidate and candidate[4]:
                        last_live, last_good = candidate, now
                    manual = 8 <= phase < 12 or 13.5 <= phase < 14.5
                    requested = 1 if manual else 0
                    command = None
                    if requested != previous_request:
                        command = {'frame': index, 'source': requested, 'at': now,
                                   'ready': bool(last_brb if manual else candidate and candidate[4])}
                        report['commands'].append(command)
                    previous_request = requested
                    if not manual and candidate and candidate[4]:
                        source, video, audio = 0, candidate[2], candidate[3]
                    elif not manual and last_live and last_good is not None and now - last_good <= 1:
                        source, video, audio = 3, last_live[2], SILENCE
                    elif last_brb and not 54 <= phase < 58:
                        source, video, audio = 1, last_brb[2], last_brb[3]
                    else:
                        source, video, audio = 2, EMERGENCY, SILENCE
                    try:
                        video_queue.put(marked_video(video, source, index), timeout=0.2)
                        audio_queue.put(audio if args.sync_markers else marked_audio(audio, index), timeout=0.2)
                    except queue.Full as error:
                        raise benchmark.ScreenError('encoder cannot sustain the bounded input queues') from error
                    report['sources'][str(source)] = report['sources'].get(str(source), 0) + 1
                    frames.write(json.dumps({'frame': index, 'source': source, 'selected_at': now,
                                             'ingress_at': candidate[0] if source == 0 else None, 'command': command}) + '\n')
                    if index % FPS == 0:
                        sample = benchmark.host_sample(encoder.pid)
                        sample['playout_rss_bytes'] = benchmark.host_sample(os.getpid())['encoder_rss_bytes']
                        sample['live_queue_frames'] = len(live.frames)
                        sample['brb_queue_frames'] = len(brb.frames)
                        sample['video_write_queue_frames'] = video_queue.qsize()
                        sample['audio_write_queue_frames'] = audio_queue.qsize()
                        report['samples'].append(sample)
                    if index and index % (FPS * 30) == 0:
                        print(json.dumps({'elapsed_seconds': index // FPS, 'source': source}), flush=True)
            for target in (video_queue, audio_queue):
                target.put(None, timeout=5)
            for worker in workers:
                worker.join(timeout=10)
            if errors or any(worker.is_alive() for worker in workers):
                raise benchmark.ScreenError('encoder did not drain its bounded queues')
            if encoder.wait(timeout=15) != 0:
                raise benchmark.ScreenError('encoder failed while draining')
        report['elapsed_seconds'] = time.monotonic() - start
        report['samples'].append(benchmark.host_sample())
        report['live_buffer_dropped_frames'] = live.dropped
        live.close()
        live = None
        brb.close()
        brb = None
        if delivered:
            report['rtmp_sinks'] = delivered.finish()
            report.update(delivery.timings(args.output, clock_anchor, args.seconds * FPS, args.sync_markers, args.target_fault))
            report['unmeasured_gates'].remove('local_rtmp_delivery')
            report['unmeasured_gates'].remove('output_arrival_latency')
            if args.sync_markers:
                report['unmeasured_gates'].remove('av_content_sync')
            if args.target_fault:
                report['unmeasured_gates'].remove('target_isolation')
        report['capture'] = benchmark.inspect_capture(SimpleNamespace(output=args.output, ffmpeg='ffmpeg', ffprobe='ffprobe', seconds=args.seconds))
        report['cpu_windows'] = benchmark.cpu_windows(report['samples'])
        report['cpu_gate'] = ('unmeasured' if not report['cpu_windows'] else
                              'pass' if all(item['host_cpu_percent'] < 75 for item in report['cpu_windows']) else 'fail')
        capture = report['capture']
        passed = (capture['decode_ok'] and capture['video_frames'] == args.seconds * FPS + report.get('warmup_frames_delivered', [0])[0]
                  and capture['timestamp_discontinuities'] == 0 and report['cpu_gate'] != 'fail'
                  and report.get('maximum_ready_command_latency_seconds', 0) <= 1
                  and report.get('maximum_live_relay_delay_seconds', 0) <= 8)
        report['status'] = 'proof_completed' if passed else 'proof_rejected'
        code = 0 if passed else 1
    except (OSError, ValueError, queue.Full, subprocess.SubprocessError, benchmark.ScreenError) as error:
        report['status'] = 'failed'
        report['error'] = str(error)
        print('playout: ' + str(error), file=sys.stderr)
    except KeyboardInterrupt:
        report['status'] = 'interrupted'
        code = 130
    finally:
        stop.set()
        if encoder:
            benchmark.stop_process(encoder)
        for source in (live, brb):
            if source:
                source.close()
        for worker in workers:
            worker.join(timeout=2)
        if delivered:
            delivered.close()
        (args.output / 'report.json').write_text(json.dumps(report, indent=2, allow_nan=False) + '\n')
    print(json.dumps({'status': report['status'], 'rollout_qualified': False}), flush=True)
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--live', type=pathlib.Path, required=True)
    parser.add_argument('--brb', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    parser.add_argument('--seconds', type=int, default=90)
    parser.add_argument('--target-fault', action='store_true', help='Short probe: disconnect one local destination once')
    parser.add_argument('--sync-markers', action='store_true', help='Inputs carry the documented source flash/chirp markers')
    parser.add_argument('--relay', type=pathlib.Path)
    parser.add_argument('--sinks', type=pathlib.Path)
    args = parser.parse_args()
    if args.target_fault and (not args.relay or not 20 <= args.seconds <= 180):
        parser.error('target-fault probes require local RTMP delivery and 20–180 seconds')
    if bool(args.relay) != bool(args.sinks) or args.relay and (not args.relay.is_file() or not args.sinks.is_file()):
        parser.error('relay and sinks must both name existing benchmark executables')
    if not args.live.is_file() or not args.brb.is_file():
        parser.error('live and BRB must be existing local files')
    if args.output.exists() or not args.output.parent.is_dir() or not 12 <= args.seconds <= 14400:
        parser.error('use a new output directory and a duration of 12–14400 seconds')
    def interrupt(signum, frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupt)
    try:
        return run(args)
    except (OSError, benchmark.ScreenError) as error:
        print('playout: ' + str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
