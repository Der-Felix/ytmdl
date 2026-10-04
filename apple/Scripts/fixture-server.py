#!/usr/bin/env python3
"""Loopback-only native UI fixture. No providers, production data or credentials."""
import argparse
from functools import lru_cache
import json
import math
import struct
import threading
import wave
import io
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs


@lru_cache(maxsize=10)
def cover(index):
    colors = [(21, 63, 103), (99, 42, 72), (27, 91, 85), (147, 95, 52), (76, 62, 128), (64, 84, 103)]
    color = colors[index % len(colors)]
    pixels = []
    for y in range(256):
        row = bytearray([0])
        for x in range(256):
            distance = math.sqrt((x-128)**2+(y-128)**2)
            factor = .4 + .6 * (1-y/256)
            if 55 < distance < 85: factor = 1.7
            row.extend(min(255, int(channel*factor)) for channel in color)
        pixels.append(row)
    def chunk(kind, data):
        return struct.pack('!I', len(data)) + kind + data + struct.pack('!I', zlib.crc32(kind+data))
    return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR', struct.pack('!2I5B', 256,256,8,2,0,0,0))+chunk(b'IDAT', zlib.compress(b''.join(pixels)))+chunk(b'IEND',b'')


audio = io.BytesIO()
with wave.open(audio, 'wb') as wav:
    wav.setnchannels(1); wav.setsampwidth(2); wav.setframerate(44100)
    wav.writeframes(b'\0\0' * 44100 * 30)
AUDIO = audio.getvalue()
MIME = 'audio/wav'
TRACKS = [{'id': 't'+str(i), 'title': name, 'artists': ['Mira' if i%2 else 'Nordlicht'], 'album': name,
           'duration_ms': 30000, 'codec': 'pcm_s16le'} for i,name in enumerate(['Nachtfahrt','Zeitlos','Fernweh','Blaue Stunde','Horizont','Lichtblick'])]
RELEASES = [{'id':'r'+str(i),'title':t['title'],'artists':t['artists'],'year':2026,'track_count_in_library':1} for i,t in enumerate(TRACKS)]
ARTISTS = [{'id':'a0','name':'Nordlicht','genres':['Elektronisch'],'track_count':3},{'id':'a1','name':'Mira','genres':['Pop'],'track_count':3}]
USER = {'id':'fixture','username':'fixture_user','display_name':'Design-Vorschau','role':'user'}


class FixtureServer(ThreadingHTTPServer):
    # Artwork and AVFoundation range requests arrive concurrently on desktop.
    # The default backlog of five can drop fixture connections during startup.
    request_queue_size = 64


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def response(self, data, status=200, mime='application/json', cookies=False):
        body = json.dumps({'data': data}).encode() if mime == 'application/json' else data
        self.send_response(status); self.send_header('Content-Type', mime)
        self.send_header('Content-Length', str(len(body))); self.send_header('Cache-Control','no-store')
        self.send_header('X-YTMDL-Fixture','1')
        if cookies:
            self.send_header('Set-Cookie','ytmdl_csrf=fixture-csrf; Path=/; Max-Age=3600')
            self.send_header('Set-Cookie','ytmdl_session=fixture-session; Path=/; Max-Age=3600; HttpOnly')
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass  # Native views can cancel artwork requests while navigating.
    def do_GET(self):
        url=urlparse(self.path); path=url.path; query=parse_qs(url.query)
        if path.endswith('/auth/status'): return self.response({'authenticated':True,'setup_required':False,'user':USER},cookies=True)
        if path.endswith('/auth/me'): return self.response(USER)
        if path.endswith('/health'): return self.response({'status':'ok','version':'fixture-only'})
        if path.endswith('/artwork'): return self.response(cover(int(path.split('/')[-2][-1]) if path.split('/')[-2][-1].isdigit() else 0),mime='image/png')
        if path.endswith('/stream'):
            if 'ytmdl_session=fixture-session' not in self.headers.get('Cookie',''): return self.response({},401)
            start=0;end=len(AUDIO)-1
            header=self.headers.get('Range','')
            if header.startswith('bytes='):
                parts=header[6:].split('-');start=int(parts[0] or 0);end=min(end,int(parts[1]) if parts[1] else end)
            body=AUDIO[start:end+1];self.send_response(206 if header else 200)
            self.send_header('Content-Type',MIME);self.send_header('Accept-Ranges','bytes')
            if header:self.send_header('Content-Range',f'bytes {start}-{end}/{len(AUDIO)}')
            self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body);return
        if path.endswith('/lyrics'): return self.response({'state':'available_plain','content':'Durch die Stadt bei Nacht\nAlles wird so leise\nFenster runter, kalter Wind\nGedanken wieder frei'})
        if path.endswith('/genres'): return self.response(['Elektronisch','Pop'])
        if path.endswith('/favorites/ids'): return self.response(['t0','t1'])
        if path.endswith('/library/search'): return self.response({'artists':ARTISTS,'releases':RELEASES[:2],'tracks':TRACKS[:2]})
        if path.endswith('/library/releases'): return self.response(RELEASES if int(query.get('offset',['0'])[0])==0 else [])
        if path.endswith('/library/artists'): return self.response(ARTISTS if int(query.get('offset',['0'])[0])==0 else [])
        if path.endswith('/library/tracks'): return self.response(TRACKS if int(query.get('offset',['0'])[0])==0 else [])
        if path.endswith('/playlists/p0'): return self.response({'tracks':TRACKS})
        if path.endswith('/playlists'): return self.response([{'id':'p0','name':'Abends unterwegs','track_count':6,'duration_ms':180000}])
        self.response({},404)
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length','0')))
        if self.path.endswith('/auth/logout'):return self.response({})
        if self.path.endswith('/device'):return self.response({'device_code':'fixture-only-opaque-secret','user_code':'ABCD-EFGH','expires_in':300,'interval':5},201)
        if self.path.endswith('/device/poll'):return self.response({'status':'authorization_pending'})
        self.response({})


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--port',type=int,default=59583)
    parser.add_argument('--audio-file', help='Optional synthetic audio fixture; never use library media')
    args=parser.parse_args()
    if args.audio_file:
        from pathlib import Path
        path = Path(args.audio_file)
        MIME = {'.opus':'audio/ogg', '.ogg':'audio/ogg', '.m4a':'audio/mp4', '.wav':'audio/wav'}[path.suffix]
        AUDIO = path.read_bytes()
    server=FixtureServer(('127.0.0.1',args.port),Handler)
    print('Loopback UI fixture ready',flush=True)
    server.serve_forever()
