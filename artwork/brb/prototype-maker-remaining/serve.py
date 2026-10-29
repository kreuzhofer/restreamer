"""Serve only the shareable prototype files, never repository or app state."""
import argparse
import re
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--bind', default='127.0.0.1')
parser.add_argument('--port', default=18861, type=int)
args = parser.parse_args()
public = Path(__file__).resolve().parent/'public'
allowed = {'index.html', 'midnight.mp4', 'debug.mp4', 'midnight.png', 'debug.png', 'concepts.json'}

class Preview(SimpleHTTPRequestHandler):
    def do_GET(self):
        name = self.path.split('?', 1)[0].lstrip('/')
        if name not in allowed | {''}:
            self.send_error(404)
            return
        # Safari/iOS media requests use byte ranges for metadata and seeking.
        if name.endswith('.mp4') and self.headers.get('Range') and (public/name).is_file():
            size = (public/name).stat().st_size
            match = re.fullmatch(r'bytes=(\d+)-(\d*)', self.headers['Range'])
            if not match:
                self.send_error(416)
                return
            start = int(match[1])
            end = min(int(match[2]) if match[2] else size-1, size-1)
            if start > end:
                self.send_error(416)
                return
            self.send_response(206)
            self.send_header('Content-Type', 'video/mp4')
            self.send_header('Accept-Ranges', 'bytes')
            self.send_header('Content-Range', f'bytes {start}-{end}/{size}')
            self.send_header('Content-Length', str(end-start+1))
            self.end_headers()
            with (public/name).open('rb') as source:
                source.seek(start)
                self.wfile.write(source.read(end-start+1))
            return
        super().do_GET()

    def list_directory(self, path):
        self.send_error(404)

handler = partial(Preview, directory=str(public))
print(f'Prototype: http://{args.bind}:{args.port}/?variant=midnight', flush=True)
ThreadingHTTPServer((args.bind, args.port), handler).serve_forever()
