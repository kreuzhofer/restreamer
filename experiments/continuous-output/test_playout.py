"""A real-media tracer for the independently clocked experimental playout."""
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest


@unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'FFmpeg required')
class PlayoutCLI(unittest.TestCase):
    def test_live_buffer_and_manual_switch_use_one_continuous_encoder(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'source.mp4'
            subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i',
                            'testsrc2=size=320x180:rate=25', '-f', 'lavfi', '-i',
                            "aevalsrc=exprs='0.1*sin(2*PI*880*t)|if(lt(mod(t,1),0.01),0.7*sin(2*PI*2000*t),0)':s=48000:c=stereo", '-t', '1',
                            '-vf', "drawbox=x=iw-32:y=ih-32:w=32:h=32:color=black:t=fill,drawbox=x=iw-32:y=ih-32:w=32:h=32:color=white:t=fill:enable='eq(mod(n,25),0)'",
                            '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac', '-ac', '2', str(source)], check=True)
            result = subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name('playout.py')),
                                     '--live', str(source), '--brb', str(source),
                                     '--output', str(root / 'run'), '--seconds', '16', '--sync-markers',
                                     '--relay', str(pathlib.Path(__file__).resolve().parents[2] / 'bin/continuous-bench-relay'),
                                     '--sinks', str(pathlib.Path(__file__).resolve().parents[2] / 'bin/continuous-bench-sinks')],
                                    capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads((root / 'run' / 'report.json').read_text())
            self.assertEqual(report['capture']['video_frames'], 400 + report['warmup_frames_delivered'][0])
            self.assertEqual(report['capture']['timestamp_discontinuities'], 0)
            self.assertTrue(report['capture']['decode_ok'])
            self.assertFalse(report['rollout_qualified'])
            self.assertEqual([item['connections'] for item in report['rtmp_sinks']], [1, 1])
            self.assertEqual(report['delivered_video_frames'], [400, 400])
            self.assertLess(report['maximum_ready_command_latency_seconds'], 1)
            self.assertEqual(report['ready_commands_checked'], 8)
            self.assertLess(report['maximum_live_relay_delay_seconds'], 8)
            self.assertLessEqual(report['maximum_av_sync_error_seconds'], 0.08)
            self.assertGreaterEqual(report['av_sync_events_checked'], 8)
            # Independently inspect visible source markers in actual decoded output.
            for frame, low, high in [(0, 60, 120), (150, 0, 40), (225, 60, 120), (350, 60, 120), (380, 0, 40)]:
                pixels = subprocess.check_output(['ffmpeg', '-v', 'error', '-i',
                    str(root / 'run' / 'capture.flv'), '-vf',
                    'select=eq(n\\,{0}),crop=8:8:0:0,format=gray'.format(frame + report['warmup_frames_delivered'][0]),
                    '-frames:v', '1', '-f', 'rawvideo', 'pipe:1'])
                level = sum(pixels) / len(pixels)
                self.assertGreaterEqual(level, low)
                self.assertLessEqual(level, high)

    def test_failed_destination_recovers_without_interrupting_healthy_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'source.mp4'
            subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i',
                            'testsrc2=size=320x180:rate=25', '-f', 'lavfi', '-i',
                            'sine=frequency=880:sample_rate=48000', '-t', '1',
                            '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac', '-ac', '2', str(source)], check=True)
            binaries = pathlib.Path(__file__).resolve().parents[2] / 'bin'
            result = subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name('playout.py')),
                                     '--live', str(source), '--brb', str(source),
                                     '--output', str(root / 'run'), '--seconds', '20', '--target-fault',
                                     '--relay', str(binaries / 'continuous-bench-relay'),
                                     '--sinks', str(binaries / 'continuous-bench-sinks')],
                                    capture_output=True, text=True, timeout=80)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads((root / 'run' / 'report.json').read_text())
            self.assertEqual([item['connections'] for item in report['rtmp_sinks']], [2, 1])
            self.assertEqual(report['delivered_video_frames'], [500])
            self.assertEqual(report['recovery']['last_frame'], 499)
            self.assertLess(report['recovery']['gap_seconds'], 10)
            self.assertFalse(report['rollout_qualified'])
