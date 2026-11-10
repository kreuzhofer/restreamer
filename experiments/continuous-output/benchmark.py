#!/usr/bin/env python3
"""Capacity screen for continuous 1080p25 output; never a rollout qualification.

Only local files are accepted. No production configuration, credentials or
network destinations are read. The output directory must not already exist.
"""
import argparse
import datetime
import hashlib
import json
import math
import os
import pathlib
import platform
import selectors
import shutil
import signal
import subprocess
import sys
import time


class ScreenError(Exception):
    pass


def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=pathlib.Path, required=True,
                        help='Representative local recording with video and audio')
    parser.add_argument('--output', type=pathlib.Path, required=True,
                        help='New directory for capture, measurements and logs')
    parser.add_argument('--seconds', type=int, default=90)
    parser.add_argument('--preset', choices=['ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium'],
                        default='veryfast')
    parser.add_argument('--bitrate-kbps', type=int, default=8000)
    parser.add_argument('--threads', type=int, default=3)
    parser.add_argument('--ffmpeg', default='ffmpeg')
    parser.add_argument('--ffprobe', default='ffprobe')
    args = parser.parse_args()
    if not args.input.is_file():
        parser.error('input must be an existing regular file')
    if not 1 <= args.seconds <= 14400:
        parser.error('seconds must be between 1 and 14400')
    if not 1000 <= args.bitrate_kbps <= 30000 or not 1 <= args.threads <= 4:
        parser.error('bitrate must be 1000–30000 kbps and threads 1–4')
    if args.output.exists() or not args.output.parent.is_dir():
        parser.error('output must be a new directory with an existing parent')
    for name in ('ffmpeg', 'ffprobe'):
        executable = shutil.which(getattr(args, name))
        if not executable:
            parser.error(name + ' executable was not found')
        setattr(args, name, executable)
    args.input = args.input.resolve()
    args.output = args.output.absolute()
    return args


def run_json(command):
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=30, check=True)
        return json.loads(result.stdout)
    except (OSError, subprocess.SubprocessError, ValueError) as error:
        raise ScreenError('media probing failed; check the local input and FFprobe installation') from error


def probe(executable, path):
    return run_json([executable, '-v', 'error', '-protocol_whitelist', 'file,pipe',
                     '-format_whitelist', 'mov,matroska,webm,avi,flv,mpegts',
                     '-show_streams', '-show_format', '-of', 'json', str(path)])


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            value.update(chunk)
    return value.hexdigest()


def host_sample(pid=None):
    """Linux host totals, not just the encoder's CPU percentage. None is unknown."""
    sample = {'monotonic': time.monotonic(), 'cpu_total': None, 'cpu_busy': None,
              'memory_available_bytes': None, 'encoder_rss_bytes': None}
    try:
        with open('/proc/stat') as handle:
            values = [int(value) for value in handle.readline().split()[1:9]]
        # guest time is already included in user/nice; do not double-count it.
        sample['cpu_total'] = sum(values)
        sample['cpu_busy'] = sum(values) - values[3] - values[4]
        with open('/proc/meminfo') as handle:
            for line in handle:
                if line.startswith('MemAvailable:'):
                    sample['memory_available_bytes'] = int(line.split()[1]) * 1024
        if pid:
            with open('/proc/{}/status'.format(pid)) as handle:
                for line in handle:
                    if line.startswith('VmRSS:'):
                        sample['encoder_rss_bytes'] = int(line.split()[1]) * 1024
    except (OSError, ValueError, IndexError):
        pass  # Unknown fields remain null and cannot qualify a gate.
    return sample


def host_info():
    info = {'system': platform.system(), 'machine': platform.machine(),
            'logical_cpus': os.cpu_count(), 'memory_total_bytes': None,
            'cpu_model': None, 'cgroup_cpu_max': None}
    try:
        with open('/proc/meminfo') as handle:
            for line in handle:
                if line.startswith('MemTotal:'):
                    info['memory_total_bytes'] = int(line.split()[1]) * 1024
        with open('/proc/cpuinfo') as handle:
            for line in handle:
                if line.startswith('model name'):
                    info['cpu_model'] = line.split(':', 1)[1].strip()
                    break
        path = pathlib.Path('/sys/fs/cgroup/cpu.max')
        if path.exists():
            info['cgroup_cpu_max'] = path.read_text().strip()
    except (OSError, ValueError):
        pass
    return info


def stop_process(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def encode(args, report):
    bitrate = str(args.bitrate_kbps) + 'k'
    command = [args.ffmpeg, '-hide_banner', '-nostdin', '-v', 'warning', '-n',
               '-stats_period', '1', '-progress', 'pipe:1',
               '-protocol_whitelist', 'file,pipe',
               '-format_whitelist', 'mov,matroska,webm,avi,flv,mpegts',
               '-threads', '2', '-re', '-stream_loop', '-1', '-i', str(args.input),
               '-t', str(args.seconds), '-map', '0:v:0', '-map', '0:a:0',
               '-filter_threads', '1', '-vf',
               'scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2,fps=25,setsar=1',
               '-c:v', 'libx264', '-threads', str(args.threads), '-preset', args.preset,
               '-tune', 'zerolatency', '-pix_fmt', 'yuv420p', '-profile:v', 'high',
               '-b:v', bitrate, '-minrate', bitrate, '-maxrate', bitrate,
               '-bufsize', str(args.bitrate_kbps * 2) + 'k',
               '-g', '50', '-keyint_min', '50', '-sc_threshold', '0',
               '-x264-params', 'nal-hrd=cbr:force-cfr=1',
               '-c:a', 'aac', '-b:a', '160k', '-ar', '48000', '-ac', '2',
               '-f', 'flv', str(args.output / 'capture.flv')]
    report['encoder_command'] = command
    start = time.monotonic()
    pending = b''
    progress = {}
    report['samples'].append(host_sample())
    with (args.output / 'encoder.log').open('wb') as log:
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=log, stdin=subprocess.DEVNULL)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                next_sample = start + 1
                while process.poll() is None or selector.get_map():
                    now = time.monotonic()
                    if now - start > args.seconds + max(30, args.seconds * 0.1):
                        raise ScreenError('encoder exceeded its bounded wall-clock deadline')
                    if now >= next_sample:
                        report['samples'].append(host_sample(process.pid))
                        next_sample = now + 1
                    for key, _ in selector.select(timeout=0.2):
                        data = os.read(key.fd, 16384)
                        if not data:
                            selector.unregister(key.fileobj)
                            continue
                        pending += data
                        if len(pending) > 65536:
                            raise ScreenError('encoder progress line exceeded the size limit')
                        while b'\n' in pending:
                            line, pending = pending.split(b'\n', 1)
                            name, _, value = line.decode('utf-8', errors='replace').partition('=')
                            progress[name] = value.strip()
                            if name == 'progress':
                                report['progress'].append(dict(progress, elapsed_seconds=time.monotonic() - start))
                                progress.clear()
                    if process.poll() is not None and not selector.get_map():
                        break
            if process.wait() != 0:
                raise ScreenError('encoder failed; see encoder.log')
        finally:
            stop_process(process)
            process.stdout.close()
            report['samples'].append(host_sample())
            report['encoder_elapsed_seconds'] = time.monotonic() - start


def inspect_capture(args):
    path = args.output / 'capture.flv'
    metadata = probe(args.ffprobe, path)
    video = next((stream for stream in metadata['streams'] if stream['codec_type'] == 'video'), {})
    audio = next((stream for stream in metadata['streams'] if stream['codec_type'] == 'audio'), {})
    capture = {'decode_ok': False, 'video_frames': 0, 'width': video.get('width'),
               'height': video.get('height'), 'video_codec': video.get('codec_name'),
               'audio_codec': audio.get('codec_name'), 'audio_rate': audio.get('sample_rate'),
               'audio_channels': audio.get('channels'), 'timestamp_discontinuities': 0,
               'maximum_frame_gap_seconds': 0, 'first_video_pts': None, 'last_video_pts': None,
               'bytes': path.stat().st_size}
    with (args.output / 'decode.log').open('wb') as log:
        try:
            result = subprocess.run([args.ffmpeg, '-v', 'error', '-nostdin', '-xerror',
                                     '-err_detect', 'explode', '-threads', '2', '-i', str(path),
                                     '-f', 'null', '-'], stderr=log, stdout=subprocess.DEVNULL,
                                    timeout=args.seconds * 2 + 60)
        except subprocess.TimeoutExpired as error:
            raise ScreenError('captured-media decoding exceeded its deadline') from error
    capture['decode_ok'] = result.returncode == 0 and (args.output / 'decode.log').stat().st_size == 0
    # Retain compact timestamp evidence on disk; keep memory independent of duration.
    frames_path = args.output / 'frame-timestamps.txt'
    with (args.output / 'probe.log').open('wb') as log, frames_path.open('wb') as frames:
        try:
            result = subprocess.run([args.ffprobe, '-v', 'error', '-threads', '2',
                                     '-select_streams', 'v:0', '-show_frames',
                                     '-show_entries', 'frame=best_effort_timestamp_time',
                                     '-of', 'compact=p=0:nk=0', str(path)],
                                    stdout=frames, stderr=log, timeout=args.seconds * 2 + 60)
        except subprocess.TimeoutExpired as error:
            raise ScreenError('capture frame inspection exceeded its deadline') from error
    if result.returncode != 0 or (args.output / 'probe.log').stat().st_size != 0:
        raise ScreenError('capture frame inspection failed; see probe.log')
    with frames_path.open() as frames:
        for line in frames:
            for field in line.strip().split('|'):
                if not field.startswith('best_effort_timestamp_time='):
                    continue
                value = float(field.split('=', 1)[1])
                if not math.isfinite(value):
                    raise ScreenError('capture contains a nonfinite frame timestamp')
                previous = capture['last_video_pts']
                if previous is None:
                    capture['first_video_pts'] = value
                else:
                    gap = value - previous
                    capture['maximum_frame_gap_seconds'] = max(capture['maximum_frame_gap_seconds'], gap)
                    if abs(gap - 0.04) > 0.001:
                        capture['timestamp_discontinuities'] += 1
                capture['last_video_pts'] = value
                capture['video_frames'] += 1
    return capture


def cpu_windows(samples):
    windows = []
    end = 0
    for start, first in enumerate(samples):
        end = max(end, start + 1)
        while end < len(samples) and samples[end]['monotonic'] - first['monotonic'] < 60:
            end += 1
        if end == len(samples):
            break
        last = samples[end]
        if first['cpu_total'] is None or last['cpu_total'] is None:
            continue
        total = last['cpu_total'] - first['cpu_total']
        if total > 0:
            windows.append({'start_seconds': first['monotonic'] - samples[0]['monotonic'],
                            'duration_seconds': last['monotonic'] - first['monotonic'],
                            'host_cpu_percent': 100 * (last['cpu_busy'] - first['cpu_busy']) / total})
    return windows


def screen(args):
    metadata = probe(args.ffprobe, args.input)
    if not all(any(stream.get('codec_type') == kind for stream in metadata.get('streams', []))
               for kind in ('video', 'audio')):
        raise ScreenError('representative input must contain both video and audio; silence is not substituted')
    # Capture plus generous mux/CBR overhead and a reserve; no full decode copy.
    required = math.ceil(args.seconds * (args.bitrate_kbps + 160) * 1000 / 8 * 1.3) + 512 * 1024 * 1024
    if shutil.disk_usage(args.output.parent).free < required:
        raise ScreenError('insufficient free disk space for the bounded capture and reserve')
    args.output.mkdir(mode=0o700)
    report = {'schema_version': 1, 'status': 'running', 'rollout_qualified': False,
              'scope': 'single_encoder_capacity_screen', 'host': host_info(),
              'input_sha256': digest(args.input), 'input_streams': metadata['streams'],
              'requested_seconds': args.seconds, 'samples': [], 'progress': [],
              'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'unmeasured_gates': ['intended_host', 'representative_media_review', 'visual_quality_review',
                                   'source_switches', 'input_outages', 'audio_outages',
                                   'destination_connections', 'target_isolation', 'encoder_recovery',
                                   'av_content_sync', 'ingress_to_egress_latency', 'bounded_input_queues',
                                   'memory_growth', 'rendering_contention', 'stop_and_ending_controls']}
    if args.seconds < 14400:
        report['unmeasured_gates'].append('four_hour_duration')
    try:
        version = subprocess.run([args.ffmpeg, '-version'], capture_output=True, text=True, check=True, timeout=10)
        report['ffmpeg_version'] = version.stdout
        encode(args, report)
        # Progress timing is a backlog warning, not an ingress/egress latency measurement.
        lag = [float(item['elapsed_seconds']) - int(item['out_time_us']) / 1000000
               for item in report['progress'] if item.get('out_time_us', '').lstrip('-').isdigit()
               and float(item['elapsed_seconds']) >= 2]
        growth = max(0, max(lag) - lag[0]) if lag else None
        report['progress_lag_growth_seconds'] = growth
        report['wall_clock_gate'] = ('pass' if report['encoder_elapsed_seconds'] <= args.seconds + 3
                                      and (growth is None or growth <= 1) else 'fail')
        report['capture'] = inspect_capture(args)
        windows = cpu_windows(report['samples'])
        report['cpu_windows'] = windows
        report['cpu_gate'] = ('unmeasured' if not windows else
                              'pass' if all(window['host_cpu_percent'] < 75 for window in windows) else 'fail')
        if not windows:
            report['unmeasured_gates'].append('cpu_headroom')
        capture = report['capture']
        media_ok = (capture['decode_ok'] and capture['timestamp_discontinuities'] == 0
                    and capture['video_frames'] == args.seconds * 25
                    and (capture['width'], capture['height']) == (1920, 1080)
                    and capture['video_codec'] == 'h264' and capture['audio_codec'] == 'aac'
                    and capture['audio_rate'] == '48000' and capture['audio_channels'] == 2)
        report['media_gate'] = 'pass' if media_ok else 'fail'
        report['status'] = 'screen_completed' if media_ok and report['cpu_gate'] != 'fail' and report['wall_clock_gate'] == 'pass' else 'screen_rejected'
        return 0 if report['status'] == 'screen_completed' else 1
    except (ScreenError, OSError, subprocess.SubprocessError, ValueError) as error:
        report['status'] = 'failed'
        report['error'] = str(error)
        print('benchmark: ' + str(error), file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        report['status'] = 'interrupted'
        return 130
    finally:
        report['finished_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        (args.output / 'report.json').write_text(json.dumps(report, indent=2, allow_nan=False) + '\n')
        print(json.dumps({'status': report['status'], 'rollout_qualified': False,
                          'report': str(args.output / 'report.json')}), flush=True)


def main():
    args = arguments()
    # SIGTERM follows the same cleanup/report path as Ctrl-C.
    def interrupt(signum, frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupt)
    try:
        return screen(args)
    except (ScreenError, OSError) as error:
        print('benchmark: ' + str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
