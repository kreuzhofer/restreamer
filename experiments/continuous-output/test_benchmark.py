"""Exercise the benchmark through its CLI and captured-media outputs."""
import json
import os
import signal
import time
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

RUNNER = pathlib.Path(__file__).with_name('benchmark.py')


def setUpModule():
    if os.environ.get('REQUIRE_FFMPEG_TESTS') == '1':
        if not shutil.which('ffmpeg') or not shutil.which('ffprobe'):
            raise RuntimeError('REQUIRE_FFMPEG_TESTS requires FFmpeg and FFprobe')


class BenchmarkCLI(unittest.TestCase):
    def test_missing_media_is_rejected_without_creating_a_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            result = subprocess.run([
                sys.executable, str(RUNNER), '--input', str(root / 'missing.mp4'),
                '--output', str(root / 'run'), '--seconds', '2',
            ], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn('input must be an existing regular file', result.stderr)
            self.assertFalse((root / 'run').exists())

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'FFmpeg required')
    def test_real_media_capture_is_decodable_without_claiming_rollout(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'moving clip.mp4'
            subprocess.run([
                'ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=25',
                '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000',
                '-t', '1', '-c:v', 'libx264', '-threads', '2', '-pix_fmt', 'yuv420p',
                '-c:a', 'aac', '-ac', '2', str(source),
            ], check=True)
            result = subprocess.run([
                sys.executable, str(RUNNER), '--input', str(source),
                '--output', str(root / 'run'), '--seconds', '4',
            ], capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads((root / 'run' / 'report.json').read_text())
            self.assertEqual(report['status'], 'screen_completed')
            self.assertFalse(report['rollout_qualified'])
            self.assertEqual(report['capture']['video_frames'], 100)
            self.assertEqual(report['capture']['width'], 1920)
            self.assertEqual(report['capture']['height'], 1080)
            self.assertTrue(report['capture']['decode_ok'])
            self.assertGreaterEqual(len(report['samples']), 3)
            self.assertIn('source_switches', report['unmeasured_gates'])
            self.assertIn('av_content_sync', report['unmeasured_gates'])
            self.assertIn('intended_host', report['unmeasured_gates'])
            # Independently decode the artifact as a caller would.
            subprocess.run(['ffmpeg', '-v', 'error', '-xerror', '-i',
                            str(root / 'run' / 'capture.flv'), '-f', 'null', '-'], check=True)

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'FFmpeg required')
    def test_encoder_failure_leaves_failed_evidence_and_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'source.mp4'
            subprocess.run([
                'ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=25',
                '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000',
                '-t', '1', '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac', '-ac', '2', str(source),
            ], check=True)
            # A failed external encoder must not become a successful screen.
            encoder = root / 'failed-encoder'
            encoder.write_text('#!/bin/sh\nif [ "$1" = "-version" ]; then echo test-encoder; exit 0; fi\nexit 17\n')
            encoder.chmod(0o700)
            result = subprocess.run([
                sys.executable, str(RUNNER), '--input', str(source),
                '--output', str(root / 'run'), '--seconds', '2', '--ffmpeg', str(encoder),
            ], capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 1, result.stderr)
            report = json.loads((root / 'run' / 'report.json').read_text())
            self.assertEqual(report['status'], 'failed')
            self.assertFalse(report['rollout_qualified'])
            self.assertIn('encoder failed', report['error'])

    def test_existing_results_are_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'input.mp4'
            source.write_bytes(b'invalid media')
            output = root / 'run'
            output.mkdir()
            report = output / 'report.json'
            report.write_text('keep my previous evidence')
            result = subprocess.run([
                sys.executable, str(RUNNER), '--input', str(source),
                '--output', str(output), '--seconds', '2',
            ], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn('output must be a new directory', result.stderr)
            self.assertEqual(report.read_text(), 'keep my previous evidence')

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'FFmpeg required')
    def test_termination_stops_encoding_and_records_interruption(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'source.mp4'
            subprocess.run([
                'ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=25',
                '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000',
                '-t', '1', '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac', '-ac', '2', str(source),
            ], check=True)
            process = subprocess.Popen([
                sys.executable, str(RUNNER), '--input', str(source),
                '--output', str(root / 'run'), '--seconds', '60',
            ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                deadline = time.monotonic() + 10
                capture = root / 'run' / 'capture.flv'
                while not capture.exists() or capture.stat().st_size == 0:
                    self.assertIsNone(process.poll(), 'benchmark exited before encoding')
                    self.assertLess(time.monotonic(), deadline, 'encoder never produced a capture')
                    time.sleep(0.05)
                process.send_signal(signal.SIGTERM)
                stdout, stderr = process.communicate(timeout=10)
                self.assertEqual(process.returncode, 130, stderr)
                report = json.loads((root / 'run' / 'report.json').read_text())
                self.assertEqual(report['status'], 'interrupted')
                self.assertFalse(report['rollout_qualified'])
                stopped_size = capture.stat().st_size
                time.sleep(0.2)
                self.assertEqual(capture.stat().st_size, stopped_size)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate()

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'FFmpeg required')
    def test_slow_encoder_cannot_pass_by_eventually_emitting_all_frames(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'source.mp4'
            subprocess.run([
                'ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=25',
                '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000',
                '-t', '1', '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac', '-ac', '2', str(source),
            ], check=True)
            encoder = root / 'slow-encoder'
            encoder.write_text('#!' + sys.executable + '\nimport os, sys, time\n'
                               'if "-progress" in sys.argv: time.sleep(5)\n'
                               'os.execv(' + repr(shutil.which('ffmpeg')) + ', ["ffmpeg"] + sys.argv[1:])\n')
            encoder.chmod(0o700)
            result = subprocess.run([
                sys.executable, str(RUNNER), '--input', str(source),
                '--output', str(root / 'run'), '--seconds', '2', '--ffmpeg', str(encoder),
            ], capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 1, result.stderr)
            report = json.loads((root / 'run' / 'report.json').read_text())
            self.assertEqual(report['status'], 'screen_rejected')
            self.assertEqual(report['media_gate'], 'pass')
            self.assertEqual(report['wall_clock_gate'], 'fail')
            self.assertFalse(report['rollout_qualified'])

    @unittest.skipUnless(sys.platform == 'linux' and os.environ.get('CONTINUOUS_BENCH_LONG_TESTS') == '1',
                         'Linux CPU sampling integration is opt-in')
    def test_linux_screen_records_complete_host_cpu_windows(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'source.mp4'
            subprocess.run([
                'ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=25',
                '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000',
                '-t', '1', '-c:v', 'libx264', '-threads', '2', '-c:a', 'aac', '-ac', '2', str(source),
            ], check=True)
            result = subprocess.run([
                sys.executable, str(RUNNER), '--input', str(source),
                '--output', str(root / 'run'), '--seconds', '65',
            ], capture_output=True, text=True, timeout=180)
            report = json.loads((root / 'run' / 'report.json').read_text())
            self.assertIn(result.returncode, (0, 1), result.stderr)
            self.assertIn(report['status'], ('screen_completed', 'screen_rejected'))
            self.assertEqual(report['media_gate'], 'pass')
            self.assertGreater(len(report['cpu_windows']), 0)
            self.assertNotEqual(report['cpu_gate'], 'unmeasured')
            for window in report['cpu_windows']:
                self.assertGreaterEqual(window['duration_seconds'], 60)
                self.assertLess(window['duration_seconds'], 63)
                self.assertGreaterEqual(window['host_cpu_percent'], 0)
                self.assertLessEqual(window['host_cpu_percent'], 100)
            self.assertFalse(report['rollout_qualified'])


if __name__ == '__main__':
    unittest.main()
