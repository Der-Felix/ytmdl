# Generator of hls_formats.json.
#
# Offline reproduction: feed synthetic YouTube-style HLS master playlists through
# yt-dlp's own m3u8 parser and info processing, exactly as --dump-json would
# print them, and record which format yt-dlp's own "bestaudio" selects. No
# network access is needed or allowed; manifest URLs use the reserved .invalid
# TLD and are not part of the output. Regenerate with the yt-dlp that ships in
# the backend image, isolated from any network:
#
#   podman run --rm --network none -v "$PWD/hls_formats.py:/f.py:ro" \
#     --entrypoint python3 ghcr.io/der-felix/ytmdl-backend:<version> /f.py > hls_formats.json
import json

import yt_dlp
from yt_dlp.extractor.common import InfoExtractor

BASE = 'https://manifest.invalid/api/manifest/hls_playlist/id/fixture'

SPLIT = f'''#EXTM3U
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MEDIA:URI="{BASE}/itag/233/playlist/index.m3u8",TYPE=AUDIO,GROUP-ID="233",NAME="Default",DEFAULT=NO,AUTOSELECT=YES
#EXT-X-MEDIA:URI="{BASE}/itag/234/playlist/index.m3u8",TYPE=AUDIO,GROUP-ID="234",NAME="Default",DEFAULT=YES,AUTOSELECT=YES
#EXT-X-STREAM-INF:BANDWIDTH=282083,CODECS="avc1.4D400C,mp4a.40.5",RESOLUTION=256x144,FRAME-RATE=25,AUDIO="233"
{BASE}/itag/269/playlist/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1353986,CODECS="avc1.4D401F,mp4a.40.2",RESOLUTION=1280x720,FRAME-RATE=25,AUDIO="234"
{BASE}/itag/232/playlist/index.m3u8
'''

MUXED = f'''#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=246440,CODECS="avc1.4d400c,mp4a.40.5",RESOLUTION=256x144,FRAME-RATE=25
{BASE}/itag/91/playlist/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1149367,CODECS="avc1.4d401f,mp4a.40.2",RESOLUTION=1280x720,FRAME-RATE=25
{BASE}/itag/95/playlist/index.m3u8
'''


class Fixture(InfoExtractor):
    IE_NAME = 'fixture'


def run(name, doc):
    ydl = yt_dlp.YoutubeDL({'quiet': True, 'no_warnings': True, 'simulate': True, 'skip_download': True})
    ie = Fixture(ydl)
    formats, _ = ie._parse_m3u8_formats_and_subtitles(doc, f'{BASE}/master.m3u8', 'mp4', m3u8_id='hls')
    info = {
        'id': 'fixtureVid1', 'title': 'Fixture Song', 'duration': 200,
        'webpage_url': 'https://www.youtube.com/watch?v=fixtureVid1',
        'extractor': 'fixture', 'extractor_key': 'Fixture', 'formats': formats,
    }
    processed = ydl.process_ie_result(info, download=False)
    dumped = ydl.sanitize_info(processed, True)  # what --dump-json prints
    keep = ('format_id', 'ext', 'acodec', 'vcodec', 'protocol', 'tbr', 'abr', 'vbr', 'resolution', 'audio_ext', 'video_ext')
    selector = ydl.build_format_selector('bestaudio')
    chosen = [f['format_id'] for f in selector({'formats': processed['formats'], 'has_merged_format': False,
                                                 'incomplete_formats': False})]
    return {
        'case': name,
        'yt_dlp_version': yt_dlp.version.__version__,
        'formats': [{k: f[k] for k in keep if k in f} for f in dumped['formats']],
        'yt_dlp_bestaudio': chosen,
    }


print(json.dumps([run('hls_split_audio_renditions', SPLIT), run('hls_muxed_only', MUXED)], indent=1))
