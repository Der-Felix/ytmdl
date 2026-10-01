#!/usr/bin/env python3
"""Synthetic catalogue served only on the isolated qualification network."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from urllib.parse import urlsplit

ARTIST = {"id": 1001, "name": "Qualification Artist", "nb_album": 1}
TRACK = {"id": 3001, "title": "Qualification Tone", "duration": 4,
         "track_position": 1, "disk_number": 1, "artist": ARTIST,
         "album": {"id": 2001, "title": "Qualification Album"}}
ALBUM = {"id": 2001, "title": "Qualification Album", "artist": ARTIST,
         "record_type": "album", "release_date": "2026-01-01", "nb_tracks": 1,
         "tracks": {"data": [TRACK], "total": 1}}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        path = urlsplit(self.path).path
        payload = {
            "/search/artist": {"data": [ARTIST], "total": 1},
            "/artist/1001": ARTIST,
            "/artist/1001/albums": {"data": [ALBUM], "total": 1},
            "/album/2001": ALBUM,
            "/album/2001/tracks": {"data": [TRACK], "total": 1},
            "/track/3001": TRACK,
            "/search": {"data": [TRACK], "total": 1},
            "/artist/27": ARTIST,
        }.get(path, {"data": [], "total": 0})
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
