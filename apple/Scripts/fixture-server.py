#!/usr/bin/env python3
"""Loopback-only native UI fixture. No providers, production data or credentials."""
import argparse
import datetime
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
           'duration_ms': FIXTURE_SECONDS * 1000, 'codec': 'pcm_s16le', 'track_number': 1, 'disc_number': 1,
           'artist_id': 'a1' if i%2 else 'a0', 'release_id': 'r'+str(i)}
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
    {'id':'rediscover','kind':'rediscover','title':'Wiederentdecken','description':'Titel, die du länger nicht gehört hast','track_ids':['t2','t5','t4']},
    {'id':'unheard','kind':'unheard','title':'Noch nicht gehört','description':'Aus deiner Mediathek, noch nie gespielt','track_ids':['t4','t2']},
    # A kind this app version does not know, with a group: "Für dich" shows it generically.
    {'id':'time_capsule','kind':'time_capsule','title':'Zeitkapsel','description':'Was du vor einem Jahr gehört hast','group':'rewind','track_ids':['t5','t3']},
]
# Server 1.11 (docs/features/listening.md): every mix carries its group, and
# artist, daypart, favourites and decade mixes join, in the server's order.
MIXES_CURRENT = [
    MIXES[0] | {'group':'made_for_you'},
    # "In Dauerschleife" stays second: the TV tests focus it on Start.
    MIXES[1] | {'group':'made_for_you'},
    {'id':'daypart-evening','kind':'daypart','title':'Dein Abend-Mix','description':'Was du abends gern hörst','group':'made_for_you','daypart':'evening','track_ids':['t1','t3','t5']},
    {'id':'artist-a0','kind':'artist','title':'Mix: Nordlicht','description':'Nordlicht und ähnliche Künstler','group':'made_for_you','artist_id':'a0','track_ids':['t0','t2','t4','t1']},
    {'id':'favorites','kind':'favorites','title':'Favoriten-Mix','description':'Deine Favoriten, lange nicht Gehörtes zuerst','group':'made_for_you','track_ids':['t0','t1']},
    MIXES[2] | {'group':'rewind'},
    {'id':'forgotten_favorites','kind':'forgotten_favorites','title':'Vergessene Favoriten','description':'Favoriten, die du seit Monaten nicht gehört hast','group':'rewind','track_ids':['t1','t0']},
    {'id':'decade-2020','kind':'decade','title':'Deine 2020er','description':'Deine Titel aus den 2020er-Jahren und mehr aus dem Jahrzehnt','group':'rewind','decade':2020,'track_ids':['t2','t3','t4']},
    MIXES[3] | {'group':'discover'},
    MIXES[4],
]
FEATURES = ['mixes.groups','mixes.artist','mixes.decade','mixes.favorites','mixes.daypart',
            'radio.seeds','radio.station','history.stats','search.lyrics','listening.preferences']
# Stored lyrics for the lyrics search (search.lyrics).
LYRICS = {'t2': 'Durch die Stadt bei Nacht\nAlles wird so leise\nFenster runter, kalter Wind\nGedanken wieder frei',
          't4': 'Kalter Kaffee am Morgen\nDie Stadt ist noch grau\nIch warte auf die blaue Stunde'}
HISTORY = [('t3', 7, '2026-11-24T18:03:04.123456789Z'), ('t4', 2, '2026-11-23T08:00:00Z')]
# GET /history?sort=frequent: most played first. Not the first titles either.
FREQUENT = [('t3', 7, '2026-11-24T18:03:04Z'), ('t5', 4, '2026-11-22T20:00:00Z'), ('t4', 2, '2026-11-23T08:00:00Z')]
ARTIST_GENRES = {a['name']: a['genres'] for a in ARTISTS}


def radio(seed, genre):
    """GET /library/radio like the server: near the seed (its artist first) or within a genre, never the seed."""
    tracks = TRACKS
    if genre: tracks = [t for t in tracks if any(g.casefold() == genre.casefold() for a in t['artists'] for g in ARTIST_GENRES.get(a, []))]
    if seed:
        base = next(t for t in TRACKS if t['id'] == seed)
        tracks = sorted((t for t in tracks if t['id'] != seed), key=lambda t: t['artists'] != base['artists'])
    return tracks


def mix_summary(mix):
    ids = mix['track_ids']
    return {key:value for key,value in mix.items() if key != 'track_ids'} | {'track_count':len(ids), 'cover_track_ids':ids[:4], 'cover_release_ids':['r'+i[1:] for i in ids[:4]]}


def history_entry(track_id, plays, when):
    return next(t for t in TRACKS if t['id'] == track_id) | {'play_count':plays, 'last_played_at':when}


def track(track_id):
    return next(t for t in TRACKS if t['id'] == track_id)


def excluded_by_preferences(item):
    """Mixes and radio never choose what the listener excluded."""
    for pref in PREFERENCES:
        if pref['kind'] == 'track' and pref['target'] == item['id']: return True
        if pref['kind'] == 'artist' and pref['target'] == item['artist_id']: return True
        if pref['kind'] == 'genre' and any(g.casefold() == pref['target'].casefold() for a in item['artists'] for g in ARTIST_GENRES.get(a, [])): return True
    return False


def station(query):
    """GET /library/radio/station: (status, body). Batches continue at the cursor without repeats."""
    value = lambda name: query.get(name, [''])[0]
    seed, artist, release, genre = value('seed'), value('artist'), value('release'), value('genre')
    if sum(1 for v in (seed, artist, release) if v) > 1: return 400, ('INVALID_REQUEST', 'Only one seed.')
    if seed:
        base = next((t for t in TRACKS if t['id'] == seed), None)
        if base is None: return 404, ('TRACK_NOT_FOUND', 'Ausgangstitel ist nicht mehr verfügbar.')
        tracks, title, info = radio(seed, genre), 'Radio: ' + base['title'], {'kind':'track','id':seed,'name':base['title']}
    elif artist:
        name = ARTIST_NAMES.get(artist)
        if name is None: return 404, ('ARTIST_NOT_FOUND', 'Künstler nicht gefunden.')
        tracks = sorted(TRACKS, key=lambda t: name not in t['artists'])
        title, info = 'Radio: ' + name, {'kind':'artist','id':artist,'name':name}
    elif release:
        item = next((r for r in RELEASES if r['id'] == release), None)
        if item is None: return 404, ('RELEASE_NOT_FOUND', 'Album nicht gefunden.')
        tracks = sorted(TRACKS, key=lambda t: (t['release_id'] != release, t['artists'] != item['artists']))
        title, info = 'Radio: ' + item['title'], {'kind':'release','id':release,'name':item['title']}
    elif genre:
        tracks, title, info = radio('', genre), 'Radio: ' + genre, {'kind':'genre','name':genre}
    else:
        tracks, title, info = list(TRACKS), 'Dein Radio', {'kind':'personal','name':'Dein Radio'}
    excluded = {i for raw in query.get('exclude', []) for i in raw.split(',') if i}
    with STATE_LOCK:
        tracks = [t for t in tracks if t['id'] not in excluded and not excluded_by_preferences(t)]
    offset = 0
    cursor = value('cursor')
    if cursor:
        try: offset = int(cursor.rsplit('.', 1)[1])
        except (IndexError, ValueError): return 400, ('INVALID_REQUEST', 'Malformed cursor.')
    # With `exclude` the earlier batches are already left out of the list.
    if excluded: offset = 0
    limit = max(1, min(100, int(value('limit') or '25')))
    batch = tracks[offset:offset+limit]
    end = offset + len(batch)
    following = (value('nonce') or 'n') + '.' + str(end) if end < len(tracks) else None
    return 200, {'title':title, 'seed':info, 'tracks':batch, 'next_cursor':following, 'remaining':max(0, len(tracks)-end)}


def statistics(period):
    """GET /history/stats: a full week, a month that starts with the history, an empty year, all time with untimed plays."""
    today = datetime.date.today()
    zone = 'Europe/Berlin'
    now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
    def stamp(value): return value.isoformat().replace('+00:00', 'Z')
    def days(count, pattern):
        return [{'start':(today - datetime.timedelta(days=count-1-i)).isoformat(), 'plays':pattern[i % len(pattern)],
                 'listening_ms':pattern[i % len(pattern)] * 200000} for i in range(count)]
    def months(count, pattern):
        result = []
        year, month = today.year, today.month
        for i in range(count):
            m = month - (count - 1 - i)
            y = year + (m - 1) // 12
            m = (m - 1) % 12 + 1
            result.append({'start':'%04d-%02d-01' % (y, m), 'plays':pattern[i % len(pattern)], 'listening_ms':pattern[i % len(pattern)] * 200000})
        return result
    tops = {
        'top_tracks':[{'track':track('t3'), 'plays':12}, {'track':track('t5'), 'plays':9}, {'track':track('t4'), 'plays':7}, {'track':track('t0'), 'plays':4}, {'track':track('t1'), 'plays':2}],
        'top_artists':[{'id':'a1','name':'Mira','plays':23,'distinct_tracks':3}, {'id':'a0','name':'Nordlicht','plays':18,'distinct_tracks':3}],
        'top_releases':[{'id':'r3','title':'Blaue Stunde','artist_id':'a1','artist_name':'Mira','plays':12},
                        {'id':'r5','title':'Lichtblick','artist_id':'a1','artist_name':'Mira','plays':9},
                        {'id':'r4','title':'Horizont','artist_id':'a0','artist_name':'Nordlicht','plays':7}],
        'top_genres':[{'name':'Pop','plays':23}, {'name':'Elektronisch','plays':18}],
    }
    base = {'period':period, 'to':stamp(now), 'time_zone':zone, 'source':'events'}
    if period == 'week':
        points = days(7, [3, 5, 0, 8, 6, 12, 7])
        return base | tops | {'from':stamp(now - datetime.timedelta(days=7)), 'since':'2026-02-03T18:21:07Z', 'complete':True,
                              'totals':{'plays':41, 'distinct_tracks':5, 'distinct_artists':2, 'listening_ms':41*200000, 'untimed_plays':0},
                              'activity':{'bucket':'day', 'points':points}}
    if period == 'month':
        since = now - datetime.timedelta(days=12)
        points = [p if p['start'] >= (today - datetime.timedelta(days=12)).isoformat() else p | {'plays':0, 'listening_ms':0} for p in days(30, [4, 2, 6, 0, 9, 3])]
        return base | tops | {'from':stamp(now - datetime.timedelta(days=30)), 'since':stamp(since), 'complete':False,
                              'totals':{'plays':sum(p['plays'] for p in points), 'distinct_tracks':5, 'distinct_artists':2, 'listening_ms':sum(p['listening_ms'] for p in points), 'untimed_plays':0},
                              'activity':{'bucket':'day', 'points':points}}
    if period == 'year':
        return base | {'from':stamp(now - datetime.timedelta(days=365)), 'since':None, 'complete':True,
                       'totals':{'plays':0, 'distinct_tracks':0, 'distinct_artists':0, 'listening_ms':0, 'untimed_plays':0},
                       'top_tracks':[], 'top_artists':[], 'top_releases':None, 'top_genres':None,
                       'activity':{'bucket':'month', 'points':months(12, [0])}}
    points = months(10, [14, 22, 9, 31, 18, 26])
    return base | tops | {'from':None, 'since':'2026-02-03T18:21:07Z', 'complete':False, 'source':'aggregate',
                          'totals':{'plays':sum(p['plays'] for p in points) + 3, 'distinct_tracks':6, 'distinct_artists':2,
                                    'listening_ms':sum(p['listening_ms'] for p in points) + 600000, 'untimed_plays':3},
                          'activity':{'bucket':'month', 'points':points}}


def lyrics_hits(text):
    """The lyrics group: every word occurs, the last as a prefix; the match as it stands in the snippet."""
    words = text.casefold().split()
    if len(text.strip()) < 3 or not words: return []
    hits = []
    for track_id, content in LYRICS.items():
        flat = content.replace('\n', ' / ')
        if all(word in flat.casefold() for word in words):
            start = flat.casefold().find(words[0])
            left = max(0, start - 20)
            snippet = ('… ' if left > 0 else '') + flat[left:left+60] + (' …' if left + 60 < len(flat) else '')
            index = snippet.casefold().find(text.strip().casefold())
            match = snippet[index:index+len(text.strip())] if index >= 0 else ''
            hits.append({'track':track(track_id), 'snippet':snippet, 'match':match})
    return hits


def preference_entry(kind, target):
    if kind == 'artist':
        name = ARTIST_NAMES.get(target)
        return None if name is None else {'kind':kind, 'target':target, 'name':name}
    if kind == 'track':
        item = next((t for t in TRACKS if t['id'] == target), None)
        return None if item is None else {'kind':kind, 'target':target, 'name':item['title'], 'artist_name':item['artists'][0]}
    if kind == 'genre' and target and len(target) <= 80:
        return {'kind':kind, 'target':target, 'name':target}
    return None


STATE_LOCK = threading.RLock()
PREFERENCES = []
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
        PREFERENCES.clear()


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
    def fail(self, status, code, message):
        body = json.dumps({'error':{'code':code, 'message':message}}).encode()
        self.send_response(status); self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body))); self.send_header('Cache-Control','no-store')
        self.end_headers()
        try: self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError): pass
    def no_content(self):
        self.send_response(204); self.send_header('Cache-Control','no-store'); self.end_headers()
    @property
    def old(self):
        """Answers like server 1.10: no `features`, none of the 1.11 endpoints."""
        return getattr(self.server, 'old', False)
    def unknown(self):
        # An older server answers an unknown path with 400, not 404.
        return self.fail(400, 'INVALID_REQUEST', 'The requested endpoint does not exist.')
    def listening(self, path, query):
        """The 1.11 listening endpoints; None when the path is none of them."""
        if path.endswith('/library/radio/station'):
            if self.old: return self.unknown()
            status, body = station(query)
            return self.response(body) if status == 200 else self.fail(status, *body)
        if path.endswith('/history/stats'):
            if self.old: return self.unknown()
            period = query.get('period', ['week'])[0]
            if period not in ('week', 'month', 'year', 'all'): return self.fail(400, 'INVALID_REQUEST', 'Unknown period.')
            return self.response(statistics(period))
        if path.endswith('/listening-preferences'):
            if self.old: return self.unknown()
            with STATE_LOCK: return self.response(list(PREFERENCES))
        return None
    def do_GET(self):
        url=urlparse(self.path); path=url.path; query=parse_qs(url.query)
        if path.endswith('/auth/status'):
            reset_state()
            return self.response({'authenticated':True,'setup_required':False,'user':USER},cookies=True)
        if path.endswith('/auth/me'): return self.response(USER)
        if path.endswith('/health'):
            if self.old: return self.response({'status':'ok','version':'1.10.1'})
            return self.response({'status':'ok','version':'1.11.0','features':FEATURES})
        if path.endswith('/library/radio/station') or path.endswith('/history/stats') or path.endswith('/listening-preferences'):
            return self.listening(path, query)
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
        if path.endswith('/library/mixes') or '/library/mixes/' in path:
            mixes = MIXES if self.old else MIXES_CURRENT
            zone = query.get('tz', [''])[0]
            if self.old and zone: return self.fail(400, 'INVALID_REQUEST', 'Unknown parameter tz.')
            if path.endswith('/library/mixes'): return self.response([mix_summary(m) for m in mixes])
            mix = next((m for m in mixes if m['id'] == path.rsplit('/',1)[1]), None)
            if mix is None: return self.response({'error':{'code':'PLAYLIST_NOT_FOUND','message':'Mix not offered today'}},404)
            with STATE_LOCK:
                tracks = [t for i in mix['track_ids'] for t in TRACKS if t['id'] == i and (self.old or not excluded_by_preferences(t))]
            return self.response(mix_summary(mix) | {'tracks':tracks})
        if path.endswith('/history'):
            entries = FREQUENT if query.get('sort', [''])[0] == 'frequent' else HISTORY
            return self.response([history_entry(*entry) for entry in entries])
        if path.endswith('/library/radio'):
            seed = query.get('seed', [''])[0]; genre = query.get('genre', [''])[0]
            if seed and not any(t['id'] == seed for t in TRACKS):
                return self.response({'error':{'code':'TRACK_NOT_FOUND','message':'Ausgangstitel ist nicht mehr verfügbar.'}},404)
            return self.response(radio(seed, genre))
        if path.endswith('/top-tracks'):
            artist = ARTIST_NAMES.get(path.split('/')[-2], '')
            return self.response([t | {'play_count':9-i, 'last_played_at':'2026-11-20T10:00:00Z'} for i,t in enumerate(t for t in TRACKS if artist in t['artists'])])
        if path.endswith('/library/search'):
            result = {'artists':ARTISTS[:2],'releases':RELEASES[:2],'tracks':TRACKS[:2]}
            if not self.old: result['lyrics'] = lyrics_hits(query.get('q', [''])[0])
            return self.response(result)
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
        if urlparse(self.path).path.endswith('/listening-preferences'):
            if self.old: return self.unknown()
            if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.fail(403, 'CSRF_INVALID', 'Missing CSRF token.')
            entry = preference_entry(body.get('kind', ''), body.get('target', ''))
            if entry is None: return self.fail(400, 'INVALID_REQUEST', 'Unknown target.')
            with STATE_LOCK:
                existing = next((p for p in PREFERENCES if p['kind'] == entry['kind'] and p['target'] == entry['target']), None)
                if existing: return self.response(existing)
                saved = entry | {'id':'pref-' + str(len(PREFERENCES) + 1) + '-' + entry['kind'],
                                 'created_at':datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace('+00:00', 'Z')}
                PREFERENCES.insert(0, saved)
            return self.response(saved, 201)
        if '/favorites/' in self.path:
            if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.response({},403)
            with STATE_LOCK: FAVORITES.add(self.path.split('/')[-1])
            return self.response({})
        return self.mutate_playlist(body)
    def do_DELETE(self):
        url = urlparse(self.path)
        if '/listening-preferences' in url.path:
            if self.old: return self.unknown()
            if self.headers.get('X-CSRF-Token') != 'fixture-csrf': return self.fail(403, 'CSRF_INVALID', 'Missing CSRF token.')
            query = parse_qs(url.query)
            identifier = url.path.rsplit('/listening-preferences', 1)[1].strip('/')
            kind, target = query.get('kind', [''])[0], query.get('target', [''])[0]
            with STATE_LOCK:
                # Idempotent: removing what is not there is fine too.
                PREFERENCES[:] = [p for p in PREFERENCES if not (p['id'] == identifier or (p['kind'] == kind and p['target'] == target))]
            return self.no_content()
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
    parser.add_argument('--old-server', action='store_true', help='Answer only like server 1.10 (no features) on --port')
    parser.add_argument('--no-old-port', action='store_true', help='Do not also answer like server 1.10 on --port + 1')
    args=parser.parse_args()
    AUDIT_FAILURES = args.audit_failures
    reset_state()
    if args.audio_file:
        from pathlib import Path
        path = Path(args.audio_file)
        MIME = {'.opus':'audio/ogg', '.ogg':'audio/ogg', '.m4a':'audio/mp4', '.wav':'audio/wav'}[path.suffix]
        AUDIO = path.read_bytes()
    server=FixtureServer(('127.0.0.1',args.port),Handler)
    server.old = args.old_server
    if not args.old_server and not args.no_old_port:
        # The same library as server 1.10 answers it, for the older-server UI test.
        older = FixtureServer(('127.0.0.1',args.port+1),Handler)
        older.old = True
        threading.Thread(target=older.serve_forever, daemon=True).start()
    print('Loopback UI fixture ready',flush=True)
    server.serve_forever()
