#!/usr/bin/env python3
"""Exercise the production continuous path using a local recording and local sinks.

This bounded smoke/capacity run is not four-hour rollout qualification. It never
reads production settings or contacts a streaming platform. Mount input read-only.
"""
import argparse
import json
import pathlib
import subprocess
import time

import benchmark
from delivery import Delivery


def run():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    parser.add_argument('--relay', type=pathlib.Path, required=True)
    parser.add_argument('--sinks', type=pathlib.Path, required=True)
    parser.add_argument('--seconds', type=int, default=180)
    args = parser.parse_args()
    if not args.input.is_file() or args.output.exists() or not 120 <= args.seconds <= 14400:
        parser.error('require an existing recording, new output directory, and 120–14400 seconds')
    args.output.mkdir(mode=0o700)
    report = {'rollout_qualified': False, 'status': 'running', 'samples': [], 'switches': [],
              'input': str(args.input), 'input_stat_before': args.input.stat().st_size,
              'unmeasured': ['four_hour_integrated_endurance', 'content_audio_video_sync',
                             'matched_compressed_ingress_delay', 'rendering_contention']}
    delivery = publisher = None
    log = (args.output / 'publisher.log').open('wb')
    try:
        delivery = Delivery(args.output, args.relay, args.sinks, args.seconds,
                            target_fault=True, continuous=True)
        def publish(offset=0):
            return subprocess.Popen(['ffmpeg', '-v', 'error', '-nostdin', '-re',
                '-ss', str(offset), '-stream_loop', '-1', '-i', str(args.input),
                '-map', '0:v:0', '-map', '0:a:0', '-c', 'copy', '-f', 'flv', delivery.url],
                stdout=subprocess.DEVNULL, stderr=log)
        publisher = publish()
        started = time.monotonic()
        actions = [(30, 'brb'), (45, 'go_live'), (65, 'input_loss'), (75, 'input_return'),
                   (95, 'brb'), (110, 'go_live')]
        next_action = 0
        while time.monotonic() - started < args.seconds:
            elapsed = time.monotonic() - started
            if next_action < len(actions) and elapsed >= actions[next_action][0]:
                action = actions[next_action][1]
                if action == 'input_loss':
                    benchmark.stop_process(publisher)
                    publisher = None
                elif action == 'input_return':
                    publisher = publish(int(elapsed))
                else:
                    delivery.command(action)
                report['switches'].append({'at_seconds': elapsed, 'action': action})
                next_action += 1
            sample = benchmark.host_sample(delivery.relay.pid)
            sample['elapsed'] = elapsed
            sample['stage'] = delivery.request('/api/stage')
            report['samples'].append(sample)
            if delivery.relay.poll() is not None:
                raise RuntimeError('production relay exited')
            time.sleep(1)
        delivery.finish()
        report['sink_summary'] = json.loads((args.output / 'sinks' / 'summary.json').read_text())
        report['captures'] = []
        for capture in sorted((args.output / 'sinks').glob('sink-*/connection-*.flv')):
            decoded = subprocess.run(['ffmpeg', '-v', 'error', '-xerror', '-i', str(capture),
                                      '-f', 'null', '-'], capture_output=True, timeout=180)
            times = []
            for line in capture.with_suffix('.jsonl').read_text().splitlines():
                entry = json.loads(line)
                times.append(entry['pts_ms'])
            report['captures'].append({'file': str(capture), 'decode_ok': decoded.returncode == 0,
                                       'decode_errors': decoded.stderr.decode()[:2000],
                                       'video_frames': len(times),
                                       'timestamp_discontinuities': sum(b-a != 40 for a,b in zip(times,times[1:]))})
        report['cpu_windows'] = benchmark.cpu_windows(report['samples'])
        report['status'] = 'smoke_completed' if bool(report['captures']) and all(c['decode_ok'] and c['video_frames'] > 0 and c['timestamp_discontinuities'] == 0 for c in report['captures']) else 'smoke_failed'
    except Exception as error:
        report['status'], report['error'] = 'failed', str(error)
    finally:
        if publisher is not None:
            benchmark.stop_process(publisher)
        if delivery is not None:
            delivery.close()
        log.close()
        report['input_stat_after'] = args.input.stat().st_size
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'status': report['status'], 'rollout_qualified': False}))
    return int(report['status'] != 'smoke_completed')


if __name__ == '__main__':
    raise SystemExit(run())
