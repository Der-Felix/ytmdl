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
import threading
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


# Cold simulator accessibility snapshots can take longer than thirty seconds.
# Keep the silent stream long enough that startup/transport checks do not race
# automatic track boundaries. End-of-track behavior has separate player tests.
FIXTURE_SECONDS = 180
audio = io.BytesIO()
with wave.open(audio, 'wb') as wav:
    wav.setnchannels(1); wav.setsampwidth(2); wav.setframerate(44100)
    wav.writeframes(b'\0\0' * 44100 * FIXTURE_SECONDS)
AUDIO = audio.getvalue()
MIME = 'audio/wav'
TRACKS = [{'id': 't'+str(i), 'title': name, 'artists': ['Mira' if i%2 else 'Nordlicht'], 'album': name,
           'duration_ms': FIXTURE_SECONDS * 1000, 'codec': 'pcm_s16le'} for i,name in enumerate(['Nachtfahrt','Zeitlos','Fernweh','Blaue Stunde','Horizont','Lichtblick'])]
RELEASES = [{'id':'r'+str(i),'title':t['title'],'artists':t['artists'],'year':2026,'track_count_in_library':1} for i,t in enumerate(TRACKS)]
ARTISTS = [{'id':'a0','name':'Nordlicht','genres':['Elektronisch'],'track_count':3},{'id':'a1','name':'Mira','genres':['Pop'],'track_count':3}]
USER = {'id':'fixture','username':'fixture_user','display_name':'Design-Vorschau','role':'user'}
STATE_LOCK = threading.RLock()
PLAYLISTS = {}
FAVORITES = set()
FAILED_COLLECTIONS = set()
AUDIT_FAILURES = False


def reset_state():
    with STATE_LOCK:
        PLAYLISTS.clear()
        PLAYLISTS['p0'] = {'id':'p0','name':'Abends unterwegs','description':'', 'track_ids':[t['id'] for t in TRACKS]}
        FAVORITES.clear(); FAVORITES.update(['t0','t1'])
        FAILED_COLLECTIONS.clear()


def playlist_detail(item):
    ids = item['track_ids']
    tracks = [next(t for t in TRACKS if t['id'] == value) for value in ids]
    if item.get('smart_rules') is not None:
        rules = item['smart_rules']
        tracks = [t for t in TRACKS if not rules.get('favorites') or t['id'] in FAVORITES][:rules.get('limit',50)]
    return {key:value for key,value in item.items() if key != 'track_ids'} | {'tracks':tracks, 'track_count':len(tracks), 'duration_ms':sum(t['duration_ms'] for t in tracks)}


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
        if path.endswith('/auth/status'):
            reset_state()
            return self.response({'authenticated':True,'setup_required':False,'user':USER},cookies=True)
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
        if path.endswith('/favorites/ids'):
            with STATE_LOCK: return self.response(sorted(FAVORITES))
        if path.endswith('/library/search'): return self.response({'artists':ARTISTS,'releases':RELEASES[:2],'tracks':TRACKS[:2]})
        if path.endswith('/library/releases'): return self.response(RELEASES if int(query.get('offset',['0'])[0])==0 else [])
        if path.endswith('/library/artists'): return self.response(ARTISTS if int(query.get('offset',['0'])[0])==0 else [])
        if path.endswith('/library/tracks'):
            if AUDIT_FAILURES and query.get('release_id') == ['r5'] and 'r5' not in FAILED_COLLECTIONS:
                FAILED_COLLECTIONS.add('r5')
                return self.response({},503)
            tracks = TRACKS
            if query.get('favorite', ['false'])[0] == 'true': tracks = [t for t in tracks if t['id'] in FAVORITES]
            if 'release_id' in query: tracks = [t for t in tracks if 'r'+t['id'][1:] == query['release_id'][0]]
            if 'artist_id' in query:
                artist = 'Nordlicht' if query['artist_id'][0] == 'a0' else 'Mira'
                tracks = [t for t in tracks if artist in t['artists']]
            offset = int(query.get('offset', ['0'])[0]); limit = int(query.get('limit', ['100'])[0])
            return self.response(tracks[offset:offset+limit])
        if '/playlists/' in path:
            with STATE_LOCK:
                item = PLAYLISTS.get(path.split('/playlists/')[1])
                return self.response(playlist_detail(item) if item else {},200 if item else 404)
        if path.endswith('/playlists'):
            with STATE_LOCK: return self.response([playlist_detail(p) for p in PLAYLISTS.values()])
        self.response({},404)
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))) or b'{}')
        if self.path.endswith('/loudness'):
            if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.response({},403)
            return self.response({'gain_db': -3, 'integrated_lufs': -11, 'true_peak_db': -1})
        if '/playlists' in self.path: return self.mutate_playlist(body)
        if self.path.endswith('/auth/logout'):return self.response({})
        if self.path.endswith('/device'):return self.response({'device_code':'fixture-only-opaque-secret','user_code':'ABCD-EFGH','expires_in':300,'interval':5},201)
        if self.path.endswith('/device/poll'):return self.response({'status':'authorization_pending'})
        self.response({})
    def do_PATCH(self):
        return self.mutate_playlist(json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))) or b'{}'))
    def do_PUT(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))) or b'{}')
        if '/favorites/' in self.path:
            if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.response({},403)
            with STATE_LOCK: FAVORITES.add(self.path.split('/')[-1])
            return self.response({})
        return self.mutate_playlist(body)
    def do_DELETE(self):
        if '/favorites/' in self.path:
            if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.response({},403)
            with STATE_LOCK: FAVORITES.discard(self.path.split('/')[-1])
            return self.response({})
        return self.mutate_playlist({})
    def mutate_playlist(self, body):
        if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.response({},403)
        if body.get('name') == 'Audit failure': return self.response({},503)
        with STATE_LOCK:
            parts = self.path.split('/playlists',1)[1].strip('/').split('/')
            if parts == ['']:
                identifier = 'p' + str(len(PLAYLISTS) + 1)
                item = {'id':identifier,'name':body['name'],'description':body.get('description',''), 'smart_rules':body.get('smart_rules'), 'track_ids':[]}
                PLAYLISTS[identifier] = item
                return self.response(playlist_detail(item),201)
            item = PLAYLISTS.get(parts[0])
            if item is None: return self.response({},404)
            if self.command == 'DELETE' and len(parts) == 1:
                del PLAYLISTS[parts[0]]; return self.response({})
            if self.command == 'PATCH': item.update(body)
            if len(parts) > 1 and parts[1] == 'rules': item['smart_rules'] = body.get('smart_rules')
            if parts[-1] == 'bulk': item['track_ids'] += [i for i in body['track_ids'] if i not in item['track_ids']]
            if parts[-1] == 'reorder': item['track_ids'] = body['track_ids']
            if self.command == 'DELETE' and len(parts) == 3 and parts[1] == 'tracks': item['track_ids'].remove(parts[2])
            return self.response(playlist_detail(item))


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--port',type=int,default=59583)
    parser.add_argument('--audio-file', help='Optional synthetic audio fixture; never use library media')
    parser.add_argument('--audit-failures', action='store_true', help='Isolated retry/error UI cases')
    args=parser.parse_args()
    AUDIT_FAILURES = args.audit_failures
    reset_state()
    if args.audio_file:
        from pathlib import Path
        path = Path(args.audio_file)
        MIME = {'.opus':'audio/ogg', '.ogg':'audio/ogg', '.m4a':'audio/mp4', '.wav':'audio/wav'}[path.suffix]
        AUDIO = path.read_bytes()
    server=FixtureServer(('127.0.0.1',args.port),Handler)
    print('Loopback UI fixture ready',flush=True)
    server.serve_forever()
