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
           'duration_ms': FIXTURE_SECONDS * 1000, 'codec': 'pcm_s16le', 'track_number': 1, 'disc_number': 1}
          for i,name in enumerate(['Nachtfahrt','Zeitlos','Fernweh','Blaue Stunde','Horizont','Lichtblick'])]
# Albums from different years; the last two are an EP and a single, so the
# artist page shows "Singles & EPs" apart from the albums.
RELEASE_TYPES = ['album','album','album','album','ep','single']
RELEASES = [{'id':'r'+str(i),'title':t['title'],'artists':t['artists'],'album_artist':t['artists'][0],'year':2020+i,
             'release_type':RELEASE_TYPES[i],'track_count_in_library':1} for i,t in enumerate(TRACKS)]
# Artists without titles fill the A–Z list: an umlaut under O, a number under #.
ARTISTS = [{'id':'a0','name':'Nordlicht','genres':['Elektronisch'],'track_count':3,'release_count':3},
           {'id':'a1','name':'Mira','genres':['Pop'],'track_count':3,'release_count':3},
           {'id':'a2','name':'Ankerplatz','genres':['Pop'],'track_count':0,'release_count':0},
           {'id':'a3','name':'Östlich','genres':['Elektronisch'],'track_count':0,'release_count':0},
           {'id':'a4','name':'42 Grad','genres':['Pop'],'track_count':0,'release_count':0}]
ARTIST_NAMES = {a['id']: a['name'] for a in ARTISTS}


def matches(text, query):
    return query.casefold() in text.casefold()
USER = {'id':'fixture','username':'fixture_user','display_name':'Design-Vorschau','role':'user'}
# Personal mixes and history as GET /library/mixes and GET /history answer
# them. History avoids the first titles: UI tests open albums by those names.
MIXES = [
    {'id':'genre-0a1b2c3d4e5f','kind':'genre','title':'Dein Mix 1','description':'Deine Favoriten aus Elektronisch und Titel, die du kaum kennst','genre':'Elektronisch','track_ids':['t0','t2','t4']},
    {'id':'on_repeat','kind':'on_repeat','title':'In Dauerschleife','description':'Deine meistgespielten Titel der letzten 30 Tage','track_ids':['t1','t3','t5']},
]
HISTORY = [('t3', 7, '2026-11-24T18:03:04.123456789Z'), ('t4', 2, '2026-11-23T08:00:00Z')]


def mix_summary(mix):
    ids = mix['track_ids']
    return {key:value for key,value in mix.items() if key != 'track_ids'} | {'track_count':len(ids), 'cover_track_ids':ids[:4], 'cover_release_ids':['r'+i[1:] for i in ids[:4]]}


def history_entry(track_id, plays, when):
    return next(t for t in TRACKS if t['id'] == track_id) | {'play_count':plays, 'last_played_at':when}
STATE_LOCK = threading.RLock()
PLAYLISTS = {}
FAVORITES = set()
FAILED_COLLECTIONS = set()
DEVICE_POLLS = {}
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
        if path.endswith('/library/mixes'): return self.response([mix_summary(m) for m in MIXES])
        if '/library/mixes/' in path:
            mix = next((m for m in MIXES if m['id'] == path.rsplit('/',1)[1]), None)
            if mix is None: return self.response({'error':{'code':'PLAYLIST_NOT_FOUND','message':'Mix not offered today'}},404)
            return self.response(mix_summary(mix) | {'tracks':[t for i in mix['track_ids'] for t in TRACKS if t['id'] == i]})
        if path.endswith('/history'): return self.response([history_entry(*entry) for entry in HISTORY])
        if path.endswith('/top-tracks'):
            artist = ARTIST_NAMES.get(path.split('/')[-2], '')
            return self.response([t | {'play_count':9-i, 'last_played_at':'2026-11-20T10:00:00Z'} for i,t in enumerate(t for t in TRACKS if artist in t['artists'])])
        if path.endswith('/library/search'): return self.response({'artists':ARTISTS[:2],'releases':RELEASES[:2],'tracks':TRACKS[:2]})
        if '/library/releases/' in path:
            release = next((r for r in RELEASES if r['id'] == path.rsplit('/',1)[1]), None)
            if release is None: return self.response({'error':{'code':'NOT_FOUND','message':'Album not found'}},404)
            artist = next((a for a in ARTISTS if a['name'] == release['album_artist']), None)
            return self.response({'release':release, 'artist':artist, 'tracks':[t for t in TRACKS if t['title'] == release['title']]})
        if path.endswith('/library/releases'):
            if int(query.get('offset',['0'])[0]) != 0: return self.response([])
            releases = RELEASES
            if 'artist_id' in query: releases = [r for r in releases if ARTIST_NAMES.get(query['artist_id'][0]) in r['artists']]
            if query.get('q', [''])[0]: releases = [r for r in releases if matches(r['title'] + ' ' + ' '.join(r['artists']), query['q'][0])]
            return self.response(releases)
        if path.endswith('/library/artists'):
            if int(query.get('offset',['0'])[0]) != 0: return self.response([])
            artists = ARTISTS
            if query.get('q', [''])[0]: artists = [a for a in artists if matches(a['name'], query['q'][0])]
            return self.response(artists)
        if path.endswith('/library/tracks'):
            if AUDIT_FAILURES and query.get('release_id') == ['r5'] and 'r5' not in FAILED_COLLECTIONS:
                FAILED_COLLECTIONS.add('r5')
                return self.response({},503)
            tracks = TRACKS
            if query.get('favorite', ['false'])[0] == 'true': tracks = [t for t in tracks if t['id'] in FAVORITES]
            if 'release_id' in query: tracks = [t for t in tracks if 'r'+t['id'][1:] == query['release_id'][0]]
            if 'artist_id' in query:
                artist = ARTIST_NAMES.get(query['artist_id'][0], '')
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
        if self.path.endswith('/device'):
            with STATE_LOCK: DEVICE_POLLS.clear()
            return self.response({'device_code':'fixture-only-opaque-secret','user_code':'ABCD-EFGH','expires_in':300,'interval':5},201)
        if self.path.endswith('/device/poll'):
            # The backend contract: pending, then a too-fast poll is told to slow down,
            # then the approved grant is exchanged for the session cookies.
            with STATE_LOCK:
                DEVICE_POLLS[body.get('device_code')] = polls = DEVICE_POLLS.get(body.get('device_code'), 0) + 1
            if polls < 3: return self.response({'status':['authorization_pending','slow_down'][polls-1]})
            return self.response({'status':'authorized'},cookies=True)
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
