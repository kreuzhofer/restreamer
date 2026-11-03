"""Check endurance evidence through generated media and the command-line interface."""
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = pathlib.Path(__file__).parent


@unittest.skipUnless(shutil.which('ffmpeg'), 'FFmpeg required')
class EnduranceMarkers(unittest.TestCase):
    def test_prepared_recording_has_decodable_frame_ids_and_sync_events(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'original.mp4'
            subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i',
                            'testsrc2=size=320x180:rate=25', '-f', 'lavfi', '-i',
                            'sine=frequency=440:sample_rate=48000', '-t', '2',
                            '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac',
                            '-ac', '2', str(source)], check=True)
            original = source.read_bytes()
            result = subprocess.run([sys.executable, str(HERE / 'endurance.py'), 'prepare',
                                     '--input', str(source), '--output', str(root / 'fixture'),
                                     '--seconds-per-sample', '2', '--offsets', '0'],
                                    capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stderr)
            fixture = root / 'fixture' / 'marked.mp4'
            pixels = subprocess.check_output(['ffmpeg', '-v', 'error', '-i', str(fixture),
                                             '-vf', 'crop=256:8:0:0,format=gray',
                                             '-fps_mode', 'passthrough', '-f', 'rawvideo', 'pipe:1'])
            frames = [pixels[n:n+2048] for n in range(0, len(pixels), 2048)]
            self.assertEqual(len(frames), 50)
            for n, frame in enumerate(frames):
                bits = [frame[3*256 + bit*8 + 3] > 128 for bit in range(32)]
                self.assertEqual(sum(int(b) << i for i,b in enumerate(bits[:8])), 165)
                self.assertEqual(sum(int(b) << i for i,b in enumerate(bits[8:28])), n)
            self.assertEqual(source.read_bytes(), original)
            verified = subprocess.run([sys.executable,str(HERE/'endurance.py'),'verify','--input',str(fixture),
                                       '--output',str(root/'verified')],capture_output=True,text=True,timeout=30)
            self.assertEqual(verified.returncode,0,verified.stderr)
            self.assertEqual(json.loads((root/'verified/report.json').read_text())['sync_events'],1)
            again = subprocess.run([sys.executable, str(HERE / 'endurance.py'), 'prepare',
                                    '--input', str(source), '--output', str(root / 'fixture'),
                                    '--seconds-per-sample', '2', '--offsets', '0'],
                                   capture_output=True, text=True)
            self.assertNotEqual(again.returncode, 0)
            manifest = json.loads((root / 'fixture' / 'manifest.json').read_text())
            self.assertEqual(manifest['frames'], 50)
            self.assertEqual(manifest['original_audio'], 'downmixed original on left; sync chirps on right')



@unittest.skipUnless(shutil.which('ffmpeg') and (HERE.parents[1] / 'bin/continuous-bench-sinks').exists(), 'capture helper required')
class EnduranceIngress(unittest.TestCase):
    def test_proxy_captures_input_and_can_drop_audio_without_closing_video(self):
        binary = str((HERE.parents[1] / 'bin/continuous-bench-sinks').resolve())
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            sinks = subprocess.Popen([binary, '-output', str(root / 'sinks'), '-seconds', '20'], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            proxy = sender = None
            try:
                target = json.loads(sinks.stdout.readline())['targets'][0]
                proxy = subprocess.Popen([binary, '-output', str(root / 'ingress'), '-seconds', '20',
                                          '-proxy-upstream', target['url'], '-proxy-key', target['stream_key']],
                                         stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                line = proxy.stdout.readline()
                self.assertTrue(line, proxy.stderr.read() if proxy.poll() is not None else 'no proxy endpoint')
                endpoint = json.loads(line)['url']
                (root / 'ingress' / 'control.json').write_text('{"drop_audio":true}')
                sender = subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i',
                                         'testsrc2=size=320x180:rate=25', '-f', 'lavfi', '-i',
                                         'sine=frequency=440:sample_rate=48000', '-t', '1',
                                         '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac',
                                         '-f', 'flv', endpoint], capture_output=True, timeout=15)
                self.assertEqual(sender.returncode, 0, sender.stderr)
            finally:
                if proxy:
                    if proxy.poll() is None:
                        proxy.terminate()
                    proxy.communicate(timeout=5)
                if sinks.poll() is None:
                    sinks.terminate()
                sinks.communicate(timeout=5)
            incoming = root / 'ingress/source/connection-001.flv'
            outgoing = root / 'sinks/sink-1/connection-001.flv'
            self.assertTrue(incoming.exists())
            self.assertTrue(outgoing.exists())
            def packets(path):
                data = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-show_packets', '-of', 'json', str(path)]))
                return data['packets']
            self.assertTrue(any(p['codec_type']=='audio' for p in packets(incoming)))
            sent = packets(outgoing)
            self.assertEqual(sum(p['codec_type']=='video' for p in sent), 25)
            self.assertFalse(any(p['codec_type']=='audio' for p in sent))

@unittest.skipUnless(shutil.which('ffmpeg'), 'FFmpeg required')
class EnduranceAnalysis(unittest.TestCase):
    def test_analysis_rejects_latency_above_ceiling_and_failed_capture(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            source=root/'original.mp4'
            subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','testsrc2=size=320x180:rate=25',
                            '-f','lavfi','-i','sine=frequency=440:sample_rate=48000','-t','3',
                            '-c:v','libx264','-threads','2','-c:a','aac','-ac','2',str(source)],check=True)
            subprocess.run([sys.executable,str(HERE/'endurance.py'),'prepare','--input',str(source),
                            '--output',str(root/'fixture'),'--seconds-per-sample','3','--offsets','0'],
                            check=True,stdout=subprocess.DEVNULL)
            media=root/'source.flv'
            subprocess.run(['ffmpeg','-v','error','-i',str(root/'fixture/marked.mp4'),'-c','copy',str(media)],check=True)
            packets=json.loads(subprocess.check_output(['ffprobe','-v','error','-select_streams','v',
                              '-show_packets','-of','json',str(media)]))['packets']
            pts=[round(float(p['pts_time'])*1000) for p in packets]
            captures=['ingress/source/connection-001.flv','sinks/sink-1/connection-001.flv',
                      'sinks/sink-1/connection-002.flv','sinks/sink-2/connection-001.flv']
            for name in captures:
                path=root/name; path.parent.mkdir(parents=True,exist_ok=True)
                shutil.copyfile(media,path)
                # Decode presentation order; capture arrivals are emitted in coding order.
                path.with_suffix('.jsonl').write_text(''.join(json.dumps({'pts_ms':p,'arrival_ns':int((100+p/1000)*1e9)})+'\n' for p in pts))
            run={'status':'captured','elapsed_seconds':3,'deferred_render':{'resumed_state':'ready'},
                 'sink_summary':[{'connections':2,'rejected':0,'write_failures':0},{'connections':1,'rejected':0,'write_failures':0}]}
            (root/'run.json').write_text(json.dumps(run))
            samples=[{'monotonic':0,'cpu_total':0,'cpu_busy':0,'continuous':{}},
                     {'monotonic':60,'cpu_total':10000,'cpu_busy':3000,'continuous':{}}]
            (root/'samples.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in samples))
            def analyze(delay):
                for name in captures[1:]:
                    (root/name).with_suffix('.jsonl').write_text(''.join(json.dumps({'pts_ms':p,'arrival_ns':int((100+p/1000+delay)*1e9)})+'\n' for p in sorted(pts)))
                return subprocess.run([sys.executable,str(HERE/'endurance.py'),'analyze','--output',str(root),'--seconds','3'],capture_output=True,text=True,timeout=60)
            good=analyze(5)
            self.assertEqual(good.returncode,0,good.stderr)
            report=json.loads((root/'report.json').read_text())
            self.assertAlmostEqual(report['maximum_ingress_to_sink_seconds'],5,places=3)
            self.assertFalse(report['rollout_qualified'])
            bad=analyze(9)
            self.assertEqual(bad.returncode,1,bad.stdout+bad.stderr)
            report=json.loads((root/'report.json').read_text())
            self.assertFalse(report['gates']['matched_delay'])
            subprocess.run(['ffmpeg','-v','error','-y','-copyts','-i',str(media),'-c:v','copy',
                            '-af','adelay=250|250','-c:a','aac',str(root/captures[-1])],check=True)
            bad_audio=analyze(5)
            self.assertEqual(bad_audio.returncode,1,bad_audio.stdout)
            self.assertIn('alignment exceeded 80 ms',bad_audio.stderr)
            shutil.copyfile(media,root/captures[-1])
            run['status']='failed'; (root/'run.json').write_text(json.dumps(run))
            failed=analyze(5)
            self.assertEqual(failed.returncode,1,'failed capture must not become successful evidence')


if __name__ == '__main__':
    unittest.main()
