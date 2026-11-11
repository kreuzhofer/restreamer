#!/usr/bin/env python3
"""Isolated production-engine endurance measurement; never contacts platforms."""
import argparse
import json
import pathlib
import subprocess
import sys


def marker_filter(first):
    filters = ['scale=1920:1080', 'fps=25', 'drawbox=x=0:y=0:w=256:h=8:color=black:t=fill']
    for bit in range(8):
        if 165 & (1 << bit):
            filters.append(f'drawbox=x={bit*8}:y=0:w=8:h=8:color=white:t=fill')
    for bit in range(20):
        filters.append(f"drawbox=x={(bit+8)*8}:y=0:w=8:h=8:color=white:t=fill:enable='mod(floor((n+{first})/{1<<bit}),2)'")
    filters += ['drawbox=x=iw-32:y=ih-32:w=32:h=32:color=black:t=fill',
                "drawbox=x=iw-32:y=ih-32:w=32:h=32:color=white:t=fill:enable='eq(mod(n,25),0)'"]
    return ','.join(filters)


def prepare(args):
    if not args.input.is_file() or args.output.exists():
        raise ValueError('input must exist and output directory must be new')
    if not 2 <= args.seconds_per_sample <= 600:
        raise ValueError('sample duration must be 2–600 seconds')
    offsets = [int(n) for n in args.offsets.split(',')]
    if not offsets or len(offsets) > 10 or any(n < 0 for n in offsets):
        raise ValueError('require 1–10 non-negative source offsets')
    probe = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-show_format', '-of', 'json', str(args.input)]))
    if max(offsets) + args.seconds_per_sample > float(probe['format']['duration']) + .01:
        raise ValueError('sample extends beyond source recording')
    args.output.mkdir(mode=0o700)
    paths = []
    chirp = r"aevalsrc=if(lt(mod(t\,1)\,0.01)\,0.7*sin(2*PI*2000*t)\,0):s=48000"
    graph = '[0:a]aformat=sample_rates=48000:channel_layouts=stereo,pan=mono|c0=0.5*c0+0.5*c1[original];[original][1:a]amerge=inputs=2[audio]'
    for index, offset in enumerate(offsets):
        path = args.output / f'sample-{index}.mp4'
        subprocess.run(['ffmpeg', '-v', 'error', '-nostdin', '-n', '-threads', '2', '-ss', str(offset),
                        '-i', str(args.input), '-f', 'lavfi', '-i', chirp,
                        '-filter_complex_threads', '1', '-filter_complex', graph,
                        '-filter_threads', '1', '-map', '0:v:0', '-map', '[audio]',
                        '-vf', marker_filter(index * args.seconds_per_sample * 25),
                        '-t', str(args.seconds_per_sample), '-c:v', 'libx264', '-threads', '2',
                        '-preset', 'veryfast', '-b:v', '12M', '-pix_fmt', 'yuv420p',
                        '-g', '50', '-bf', '2', '-c:a', 'aac', '-b:a', '192k', '-ar', '48000',
                        '-ac', '2', str(path)], check=True, timeout=args.seconds_per_sample*4+60)
        paths.append(path.name)
    concat = args.output / 'samples.txt'
    concat.write_text(''.join(f"file '{name}'\n" for name in paths))
    subprocess.run(['ffmpeg', '-v', 'error', '-nostdin', '-n', '-f', 'concat', '-safe', '1',
                    '-i', str(concat), '-c', 'copy', str(args.output / 'marked.mp4')], check=True)
    manifest = {'source': str(args.input), 'source_size': args.input.stat().st_size,
                'source_mtime_ns': args.input.stat().st_mtime_ns, 'offsets_seconds': offsets,
                'seconds_per_sample': args.seconds_per_sample,
                'frames': len(offsets)*args.seconds_per_sample*25,
                'original_audio': 'downmixed original on left; sync chirps on right',
                'markers': '8-bit magic 165, 20-bit frame ID in top 256x8 strip; bottom-right flash each second'}
    (args.output / 'manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
    print(json.dumps(manifest))


# Capture analysis uses the input's decoded marker IDs, not predicted timestamps
# or an assumption about the remuxer's handling of loops and B-frames.
def marked_frames(path):
    import threading
    log_path = path.with_suffix('.decode.log')
    with log_path.open('wb') as log:
        process = subprocess.Popen(['ffmpeg', '-v', 'error', '-nostdin', '-xerror', '-threads', '2',
            '-i', str(path), '-filter_complex_threads', '1', '-filter_complex',
            '[0:v]split=2[a][b];[a]crop=256:8:0:0,format=gray[top];'
            '[b]crop=32:8:iw-32:ih-32,format=gray,pad=256:8:0:0:black[bottom];'
            '[top][bottom]vstack[out]', '-map', '[out]', '-fps_mode', 'passthrough',
            '-f', 'rawvideo', 'pipe:1'], stdout=subprocess.PIPE, stderr=log)
        timeout = threading.Timer(14400, process.kill)
        timeout.start()
        try:
            with path.with_suffix('.jsonl').open() as events_file:
                ordered_events = sorted((json.loads(line) for line in events_file), key=lambda e:e['pts_ms'])
                if len(ordered_events) > 400000:
                    raise ValueError('capture exceeds the bounded four-hour evidence limit')
                events = iter(ordered_events)
                for pixels in iter(lambda: process.stdout.read(4096), b''):
                    if len(pixels) != 4096:
                        raise ValueError('truncated decoded marker')
                    event = next(events, None)
                    if event is None:
                        raise ValueError('decoded frame has no arrival record')
                    bits = [pixels[3*256+bit*8+3] > 128 for bit in range(32)]
                    magic = sum(int(b) << i for i,b in enumerate(bits[:8]))
                    frame_id = sum(int(b) << i for i,b in enumerate(bits[8:28])) if magic == 165 and not any(bits[28:]) else None
                    yield event, frame_id, pixels[11*256+3] > 200
                if next(events, None) is not None:
                    raise ValueError('capture has arrival records without decoded frames')
            if process.wait() != 0:
                raise ValueError('captured video did not decode cleanly: '+str(log_path))
        finally:
            timeout.cancel()
            if process.poll() is None:
                process.kill()
            process.wait()
            process.stdout.close()
    if log_path.stat().st_size:
        raise ValueError('captured video reported errors: '+str(log_path))


def analyze(args):
    import bisect
    import collections
    import statistics
    import benchmark
    from delivery import audio_sync
    root = args.output
    report = json.loads((root / 'run.json').read_text())
    if report.get('status') != 'captured':
        raise ValueError('only a completed capture can be analyzed as passing evidence')
    report['analysis_status'] = 'running'
    arrivals = collections.defaultdict(list)
    source_frames = 0
    for capture in sorted((root / 'ingress/source').glob('connection-*.flv')):
        for event, marker, _ in marked_frames(capture):
            if marker is None:
                raise ValueError('input fixture lost its frame marker')
            arrivals[marker].append(event['arrival_ns'])
            source_frames += 1
    if not arrivals:
        raise ValueError('no measured input frames')
    for values in arrivals.values():
        values.sort()
    captures = []
    all_errors, all_delays = [], []
    command_checks = []
    for capture in sorted((root / 'sinks').glob('sink-*/connection-*.flv')):
        count = gaps = marked = 0
        previous = None
        first_pts = last_pts = None
        previous_flash = False
        flashes = []
        delays = []
        commands = [dict(a) for a in report.get('actions',[]) if a['action'] in ('brb','go_live')] if capture.parent.name=='sink-2' else []
        command_index = 0
        for event, marker, flash in marked_frames(capture):
            pts = event['pts_ms']
            if previous is not None and pts-previous != 40:
                gaps += 1
            previous = last_pts = pts
            if first_pts is None:
                first_pts = pts
            count += 1
            if command_index < len(commands):
                command = commands[command_index]
                if event['arrival_ns'] >= command['at_unix_ns'] and (marker is None) == (command['action']=='brb'):
                    command_checks.append({'action':command['action'],'at_seconds':command['at_seconds'],
                                           'output_latency_seconds':(event['arrival_ns']-command['at_unix_ns'])/1e9})
                    command_index += 1
            current_flash = marker is not None and flash
            if current_flash and not previous_flash and pts-first_pts > 250:
                flashes.append(pts/1000)
            previous_flash = current_flash
            if marker is None:
                continue
            candidates = arrivals.get(marker, [])
            index = bisect.bisect_right(candidates, event['arrival_ns'])-1
            if index < 0:
                raise ValueError('output marker has no preceding input arrival')
            delays.append((event['arrival_ns']-candidates[index])/1e9)
            marked += 1
        if not count or not marked or not flashes:
            raise ValueError('capture lacks measured media: '+str(capture))
        errors = audio_sync(capture, flashes, capture.with_suffix('.audio.log'), args.seconds)
        all_errors.extend(errors)
        all_delays.extend(delays)
        captures.append({'file': str(capture.relative_to(root)), 'frames': count,
                         'marked_frames': marked, 'cadence_gaps': gaps,
                         'media_seconds': (last_pts-first_pts+40)/1000,
                         'av_events': len(errors), 'maximum_av_error_seconds': max(errors),
                         'maximum_ingress_to_sink_seconds': max(delays)})
        (root / 'analysis-progress.json').write_text(json.dumps({'analyzed': captures}, indent=2))
    samples = [json.loads(line) for line in (root / 'samples.jsonl').read_text().splitlines()]
    windows = benchmark.cpu_windows(samples)
    memories = [s['anonymous_memory_bytes'] for s in samples if s.get('elapsed',0) >= 600 and s.get('anonymous_memory_bytes') is not None]
    quartile = max(1,len(memories)//4)
    growth = statistics.median(memories[-quartile:])-statistics.median(memories[:quartile]) if memories else None
    maximum = lambda name: max((s.get('continuous',{}).get(name,0) for s in samples), default=0)
    def command_gate(action, limit):
        measured = [c for c in command_checks if c['action'] == action]
        requested = [a for a in report.get('actions', []) if a['action'] == action]
        return len(measured) == len(requested) and all(0 <= c['output_latency_seconds'] <= limit for c in measured)
    gates = {'four_hours': report.get('elapsed_seconds',0) >= 14400,
             'decoding_and_cadence': bool(captures) and all(c['cadence_gaps']==0 for c in captures),
             'healthy_capture_duration': any(c['file'].startswith('sinks/sink-2/') and c['media_seconds'] >= args.seconds for c in captures),
             'matched_delay': bool(all_delays) and max(all_delays) <= 8,
             'av_sync': bool(all_errors) and max(all_errors) <= .080001,
             'cpu_headroom': bool(windows) and max(w['host_cpu_percent'] for w in windows) < 75,
             'bounded_queues': maximum('live_buffer_frames') <= 130 and maximum('prepared_buffer_frames') <= 50,
             'no_encoder_restart': maximum('restarts') == 0,
             'ready_brb_response': command_gate('brb', 1),
             'go_live_total_response': command_gate('go_live', 2),
             'no_source_overflow': maximum('source_queue_overflows') == 0,
             'deferred_render_resumed': report.get('deferred_render',{}).get('resumed_state') == 'ready',
             'destination_isolation': len(report.get('sink_summary',[])) == 2 and all(item['connections']==count and item['rejected']==0 and item['write_failures']==0 for item,count in zip(report.get('sink_summary',[]),[2,1])) and sum(c['file'].startswith('sinks/sink-1/') for c in captures)==2 and sum(c['file'].startswith('sinks/sink-2/') for c in captures)==1}
    report.update({'analysis_status': 'completed', 'source_frames': source_frames,
                   'captures': captures, 'cpu_windows': windows, 'gates': gates,
                   'memory_after_warmup': {'first_quarter_median_bytes': statistics.median(memories[:quartile]) if memories else None,
                                          'last_quarter_median_bytes': statistics.median(memories[-quartile:]) if memories else None,
                                          'change_bytes': growth, 'peak_bytes': max(memories) if memories else None},
                   'maximum_ingress_to_sink_seconds': max(all_delays),
                   'median_ingress_to_sink_seconds': statistics.median(all_delays),
                   'maximum_av_error_seconds': max(all_errors),
                   'av_events': len(all_errors), 'commands':command_checks,
                   'command_measurement': 'command submission to first changed-source frame at sink; Go Live includes fresh-keyframe readiness wait; post-readiness latency is not measured separately',
                   'rollout_qualified': False,
                   'remaining_review': ['memory trend', 'final UI acceptance',
                                        'combine with isolated encoder-fault and ending-drain regression evidence'],
                   'measurement': 'decoded input/output frame IDs matched by ingress-proxy receipt and sink arrival; includes loopback forwarding overhead'})
    measured = {k:v for k,v in gates.items() if k != 'four_hours'}
    report['status'] = ('endurance_measured' if gates['four_hours'] else 'short_measured') if all(measured.values()) else 'rejected'
    (root / 'report.json').write_text(json.dumps(report, indent=2)+'\n')
    print(json.dumps({'status': report['status'], 'gates': gates}))
    return report['status'] != 'rejected'


def run_endurance(args):
    import datetime
    import os
    import select
    import signal
    import time
    import benchmark
    from delivery import Delivery
    if not args.input.is_file() or args.output.exists() or not 120 <= args.seconds <= 14400:
        raise ValueError('require existing marked input, new output directory, and 120–14400 seconds')
    args.output.mkdir(mode=0o700)
    report = {'status': 'starting', 'seconds': args.seconds, 'input': str(args.input),
              'source_stat_before': [args.input.stat().st_size,args.input.stat().st_mtime_ns],
              'environment': benchmark.host_info(), 'actions': [], 'rollout_qualified': False}
    delivery = proxy = publisher = None
    logs = []
    samples = collections.deque(maxlen=122)
    def save(name, value):
        tmp = args.output / (name+'.tmp')
        tmp.write_text(json.dumps(value, indent=2)+'\n')
        tmp.replace(args.output / name)
    def interrupted(*_):
        raise InterruptedError('endurance run interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        delivery = Delivery(args.output, args.relay, args.sinks, args.seconds+30, target_fault=True, continuous=True)
        upstream, key = delivery.url.rsplit('/',1)
        proxy_log = (args.output / 'ingress.log').open('wb'); logs.append(proxy_log)
        proxy = subprocess.Popen([str(args.sinks), '-output', str(args.output/'ingress'),
                                  '-seconds', str(min(14500,args.seconds+100)),
                                  '-proxy-upstream', upstream, '-proxy-key', key], stdout=subprocess.PIPE, stderr=proxy_log)
        if not select.select([proxy.stdout],[],[],10)[0]:
            raise ValueError('ingress proxy did not start')
        endpoint = json.loads(proxy.stdout.readline())['url']
        publisher_log = (args.output / 'publisher.log').open('wb'); logs.append(publisher_log)
        def publish():
            return subprocess.Popen(['ffmpeg','-v','error','-nostdin','-re','-stream_loop','-1',
                                     '-i',str(args.input),'-map','0:v:0','-map','0:a:0','-c','copy','-f','flv',endpoint],
                                     stdout=subprocess.DEVNULL,stderr=publisher_log)
        publisher = publish()
        deadline = time.monotonic()+30
        while delivery.request('/api/stage')['continuous']['source'] != 'obs':
            if time.monotonic() > deadline:
                raise ValueError('continuous OBS did not become ready')
            time.sleep(.1)
        draft = delivery.request('/api/generator/designs', {'name':'Endurance capacity probe','stage':'prestream','theme':{'id':'arcade-after-hours','revision':2},
                    'scenes':[{'id':'title','layout':'title','text':'Capacity probe','duration_seconds':1}]})
        job = delivery.request('/api/generator/jobs', {'design_id':draft['id'],'version':draft['version']})
        report['deferred_render'] = {'id':job['id'], 'design_revision':job['design_revision']}
        clock_wall, clock_mono = time.time_ns(), time.monotonic_ns()
        def observation_time():
            return clock_wall + time.monotonic_ns() - clock_mono
        report['observation_clock'] = {'unix_anchor_ns':clock_wall,'monotonic_anchor_ns':clock_mono}
        started = time.monotonic()
        report['started_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        report['expected_finish_utc'] = (datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=args.seconds)).isoformat()
        report['status'] = 'running'
        save('run.json',report)
        actions = []
        schedule = [(15,'brb'),(25,'go_live'),(40,'audio_loss'),(52,'audio_return'),(70,'input_loss'),(82,'input_return')] if args.seconds < 600 else [(120,'brb'),(140,'go_live'),(240,'audio_loss'),(252,'audio_return'),(360,'input_loss'),(372,'input_return')]
        for cycle in range(0,args.seconds,600):
            actions += [(cycle+at,action) for at,action in schedule if cycle+at < args.seconds-10]
        next_action = 0
        audio_outage = False
        with (args.output/'samples.jsonl').open('w', buffering=1) as evidence:
            while time.monotonic()-started < args.seconds:
                elapsed = time.monotonic()-started
                if next_action < len(actions) and elapsed >= actions[next_action][0]:
                    action = actions[next_action][1]
                    before = observation_time()
                    if action in ('audio_loss','audio_return'):
                        audio_outage = action=='audio_loss'
                        tmp = args.output/'ingress/control.tmp'
                        tmp.write_text(json.dumps({'drop_audio':action=='audio_loss'}))
                        tmp.replace(args.output/'ingress/control.json')
                        if action=='audio_return' and publisher is None:
                            publisher=publish()
                    elif action == 'input_loss':
                        benchmark.stop_process(publisher); publisher=None
                    elif action == 'input_return':
                        publisher=publish()
                    else:
                        delivery.command(action)
                    report['actions'].append({'at_seconds':elapsed,'at_unix_ns':before,'action':action})
                    next_action += 1
                    save('run.json',report)
                state = delivery.request('/api/stage')
                sample = benchmark.host_sample(delivery.relay.pid)
                sample.update({'elapsed':elapsed,'continuous':state['continuous'],'stage':state['stage']})
                memory = pathlib.Path('/sys/fs/cgroup/memory.stat')
                sample['anonymous_memory_bytes'] = None
                if memory.exists():
                    fields = dict(line.split() for line in memory.read_text().splitlines())
                    sample['anonymous_memory_bytes'] = int(fields['anon'])
                queued = delivery.request('/api/generator/jobs/'+job['id'])
                sample['preparation'] = {'state':queued['state'],'progress':queued['progress'],'message':queued['message']}
                if elapsed > 5 and (queued['state'] not in ('queued','running') or queued['progress'] != 0 or 'capacity' not in queued['message']):
                    raise ValueError('heavy preparation was not visibly deferred')
                if delivery.relay.poll() is not None or proxy.poll() is not None:
                    raise ValueError('relay or ingress observer exited unexpectedly')
                if publisher is not None and publisher.poll() is not None:
                    if not audio_outage or publisher.returncode < 0:
                        raise ValueError('publisher exited outside the injected audio outage: '+str(publisher.returncode))
                    report['actions'].append({'at_seconds':elapsed,'at_unix_ns':observation_time(),
                                              'action':'publisher_evicted_for_audio_loss','exit_code':publisher.returncode})
                    publisher=None
                    save('run.json',report)
                if os.statvfs(args.output).f_bavail * os.statvfs(args.output).f_frsize < 10*1024**3:
                    raise ValueError('endurance stopped to preserve 10 GiB of disk headroom')
                evidence.write(json.dumps(sample)+'\n'); samples.append(sample)
                if int(elapsed)%10 == 0:
                    save('progress.json', {'elapsed_seconds':elapsed,'expected_finish_utc':report['expected_finish_utc'],
                         'latest':sample,'actions_completed':next_action})
                if len(samples)>120:
                    recent=benchmark.cpu_windows(list(samples))
                    if recent and all(w['host_cpu_percent'] > 85 for w in recent):
                        raise ValueError('endurance stopped after sustained host CPU above 85 percent')
                time.sleep(1)
        report['elapsed_seconds'] = time.monotonic()-started
        delivery.command('stop_now')
        deadline = time.monotonic()+60
        while True:
            resumed = delivery.request('/api/generator/jobs/'+job['id'])
            if resumed['state']=='ready': break
            if resumed['state'] in ('failed','cancelled') or time.monotonic()>deadline:
                raise ValueError('deferred render did not finish after Stop')
            time.sleep(.2)
        if resumed['design_revision'] != job['design_revision']:
            raise ValueError('resumed render changed captured revision')
        report['deferred_render']['resumed_state']=resumed['state']
        delivery.finish()
        report['sink_summary']=json.loads((args.output/'sinks/summary.json').read_text())
        report['status']='captured'
    except Exception as error:
        report['status']='failed'; report['error']=str(error)
    finally:
        if publisher is not None: benchmark.stop_process(publisher)
        if proxy is not None:
            benchmark.stop_process(proxy)
            proxy.stdout.close()
        if delivery is not None: delivery.close()
        for log in logs: log.close()
        report['source_stat_after']=[args.input.stat().st_size,args.input.stat().st_mtime_ns]
        save('run.json',report)
    if report['status']!='captured':
        raise ValueError(report.get('error','capture failed'))
    return analyze(args)



def verify_fixture(args):
    import os
    from delivery import audio_sync
    if not args.input.is_file() or args.output.exists():
        raise ValueError('require a marked input and a new verification directory')
    args.output.mkdir(mode=0o700)
    capture=args.output/'fixture.mp4'
    os.link(args.input,capture)
    probe=json.loads(subprocess.check_output(['ffprobe','-v','error','-select_streams','v:0',
                     '-show_packets','-show_entries','packet=pts_time','-of','json',str(capture)]))
    capture.with_suffix('.jsonl').write_text(''.join(json.dumps({'pts_ms':round(float(p['pts_time'])*1000),'arrival_ns':0})+'\n' for p in probe['packets']))
    flashes=[]; count=0; first=None; previous=False; last=0
    for event,marker,flash in marked_frames(capture):
        if marker != count:
            raise ValueError('prepared fixture frame IDs are not consecutive')
        last=event['pts_ms']/1000
        if first is None: first=last
        if flash and not previous and last-first > .25: flashes.append(last)
        previous=flash; count+=1
    errors=audio_sync(capture,flashes,args.output/'audio.log',last+1)
    if not errors:
        raise ValueError('fixture has no measurable audio/video events')
    report={'frames':count,'sync_events':len(errors),'maximum_av_error_seconds':max(errors),'status':'verified'}
    (args.output/'report.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report))
    return True

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    prep = commands.add_parser('prepare')
    prep.add_argument('--input', type=pathlib.Path, required=True)
    prep.add_argument('--output', type=pathlib.Path, required=True)
    prep.add_argument('--seconds-per-sample', type=int, default=120)
    prep.add_argument('--offsets', default='0,1800,3600,5400,7200')
    verify=commands.add_parser('verify')
    verify.add_argument('--input',type=pathlib.Path,required=True)
    verify.add_argument('--output',type=pathlib.Path,required=True)
    for name in ('run','analyze'):
        sub=commands.add_parser(name)
        sub.add_argument('--output',type=pathlib.Path,required=True)
        sub.add_argument('--seconds',type=int,default=14400)
        if name=='run':
            sub.add_argument('--input',type=pathlib.Path,required=True)
            sub.add_argument('--relay',type=pathlib.Path,required=True)
            sub.add_argument('--sinks',type=pathlib.Path,required=True)
    args=parser.parse_args()
    try:
        if args.command=='prepare': prepare(args); return 0
        if args.command=='verify': verify_fixture(args); return 0
        return 0 if (run_endurance(args) if args.command=='run' else analyze(args)) else 1
    except (ValueError,OSError,subprocess.SubprocessError) as error:
        print(str(error),file=sys.stderr)
        return 1


if __name__=='__main__':
    raise SystemExit(main())
