#!/usr/bin/env python3
"""Build an original, non-proprietary example brand package with stdlib only."""
import argparse
import hashlib
import json
import struct
import zlib
import zipfile


def png(width, height, pixel):
    def chunk(kind, data):
        return struct.pack('!I', len(data)) + kind + data + struct.pack('!I', zlib.crc32(kind + data))
    raw = b''.join(b'\0' + b''.join(bytes(pixel(x, y)) for x in range(width)) for y in range(height))
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('!2I5B', width, height, 8, 6, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b'')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', help='ZIP to create (refuses to overwrite)')
    args = parser.parse_args()
    files = {
        'assets/logo.png': png(160, 80, lambda x, y: (167, 235, 204, 255) if 12 <= x < 148 and 12 <= y < 68 else (0, 0, 0, 0)),
        'assets/border.png': png(1920, 1080, lambda x, y: (167, 235, 204, 255) if min(x, y, 1919-x, 1079-y) < 12 else (0, 0, 0, 0)),
    }
    ref = lambda name: {'id': name, 'revision': 1}
    style = dict(font='go-sans', font_size=80, list_font_size=48, background_color='#17191C', text_color='#F0F1F3', accent_color='#A7EBCC', logo=ref('logo'), logo_position='top-left', logo_height_percent=5, logo_inset_percent=5, obs_border=ref('border'), content_region=dict(width_percent=80, height_percent=60), line_spacing_percent=120, list_spacing_percent=25, border_style='none', border_width=2, effect='none', effect_speed=8)
    templates = []
    for stage, title in [('prestream', 'Starting soon'), ('brb', 'Be right back'), ('ending', 'Thanks for watching')]:
        templates.append(dict(id=stage, name=title, theme='primary', content=dict(stage=stage, scenes=[dict(id='title', layout='title', text=title, duration_seconds=8)])))
    manifest = dict(format_version=1, id='example-studio', version=1, name='Example studio', notes='Original sample artwork. Free to reuse. Replace it with your own authorised brand assets.', assets=[dict(id=name, name=name.title(), kind='image', path='assets/'+name+'.png', sha256=hashlib.sha256(files['assets/'+name+'.png']).hexdigest(), source='Original example geometry') for name in ['logo', 'border']], themes=[dict(id='primary', name='Example studio', style=style)], templates=templates)
    with zipfile.ZipFile(args.output, 'x', compression=zipfile.ZIP_DEFLATED) as archive:
        archive.writestr('manifest.json', json.dumps(manifest, indent=2))
        for path, data in files.items():
            archive.writestr(path, data)
    print(args.output)


if __name__ == '__main__':
    main()
