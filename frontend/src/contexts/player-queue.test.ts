import { afterEach, describe, expect, it } from 'bun:test'

import { loadPlayerState } from '@/lib/audio/player-storage'
import type { PlayerState, QueueTrack } from '@/lib/audio/types'
import type { LibraryTrack } from '@/types/api'
import {
  INITIAL_PLAYER_STATE,
  autoplaySeed,
  moveQueueEntry,
  playArtistAction,
  playerReducer,
  queueOrigin,
  radioContext,
  userInsertIndex,
  wantsAutoplay,
} from './player-reducer'
import type { PlayerAction } from './player-reducer'

const base: LibraryTrack = {
  id: 'x',
  title: 'X',
  artists: ['Artist'],
  album: 'Album',
  album_artist: 'Artist',
  release_id: 'rel',
  track_number: 1,
  track_total: 9,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 180000,
  year: 2026,
  source_provider: 'fixture',
  source_id: '',
  source_url: '',
  created_at: '',
}
const t = (id: string): LibraryTrack => ({ ...base, id, title: id.toUpperCase() })
const album = ['a1', 'a2', 'a3', 'a4', 'a5'].map(t)
const albumContext = { kind: 'album' as const, id: 'rel', title: 'Album' }

const run = (state: PlayerState, ...actions: PlayerAction[]) => actions.reduce(playerReducer, state)
const ids = (queue: QueueTrack[]) => queue.map((x) => x.id)
const origins = (queue: QueueTrack[]) => queue.map((x) => queueOrigin(x)[0]).join('')
const playing = () =>
  run(INITIAL_PLAYER_STATE, { type: 'PLAY_ALBUM', payload: { tracks: album, startIndex: 0, context: albumContext } })

describe('queue origins: insert positions', () => {
  it('queues after what the user queued before, ahead of the rest of the album', () => {
    const s = run(
      playing(),
      { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } },
      { type: 'ADD_TO_QUEUE', payload: { tracks: [t('u2'), t('u3')] } },
    )
    expect(ids(s.queue)).toEqual(['a1', 'u1', 'u2', 'u3', 'a2', 'a3', 'a4', 'a5'])
    expect(origins(s.queue)).toBe('cuuucccc')
    expect(s.queueContext).toEqual(albumContext)
    expect(userInsertIndex(s.queue, s.queueIndex)).toBe(4)
  })

  it('plays next directly after the current track, ahead of earlier queued tracks', () => {
    const s = run(
      playing(),
      { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } },
      { type: 'PLAY_NEXT', payload: { track: t('n1') } },
    )
    expect(ids(s.queue)).toEqual(['a1', 'n1', 'u1', 'a2', 'a3', 'a4', 'a5'])
    expect(origins(s.queue)).toBe('cuuc' + 'ccc')
  })

  it('keeps inserting after the current track once playback moved on', () => {
    const s = run(
      playing(),
      { type: 'NEXT', payload: { manual: true } },
      { type: 'NEXT', payload: { manual: true } },
      { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } },
    )
    expect(ids(s.queue)).toEqual(['a1', 'a2', 'a3', 'u1', 'a4', 'a5'])
  })

  it('starts playback with the added tracks when nothing plays', () => {
    const s = run(INITIAL_PLAYER_STATE, { type: 'ADD_TO_QUEUE', payload: { tracks: [t('u1'), t('u2')] } })
    expect(s.currentTrack?.id).toBe('u1')
    expect(origins(s.queue)).toBe('cc')
    expect(s.queueContext).toBeNull()
  })

  it('takes the origin marks off when a queued track starts a new queue', () => {
    const queued = run(playing(), { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } })
    const marked = queued.queue[1]!
    expect(marked.queueOrigin).toBe('user')
    const s = run(queued, { type: 'PLAY_TRACK', payload: { track: marked, queue: queued.queue, queueIndex: 1 } })
    expect(origins(s.queue)).toBe('cccccc')
    expect(s.currentTrack).not.toHaveProperty('queueOrigin')
  })
})

describe('queue origins: moving and removing', () => {
  it('makes a title dropped directly after the current track "Als Nächstes"', () => {
    const s = run(playing(), { type: 'REORDER_QUEUE', payload: { fromIndex: 3, toIndex: 1 } })
    expect(ids(s.queue)).toEqual(['a1', 'a4', 'a2', 'a3', 'a5'])
    expect(origins(s.queue)).toBe('cuccc')
  })

  it('gives a moved title the origin of the title it lands after', () => {
    const queued = run(playing(), { type: 'ADD_TO_QUEUE', payload: { tracks: [t('u1'), t('u2')] } })
    // A context title dropped between two queued ones joins them.
    const into = run(queued, { type: 'REORDER_QUEUE', payload: { fromIndex: 4, toIndex: 2 } })
    expect(ids(into.queue)).toEqual(['a1', 'u1', 'a3', 'u2', 'a2', 'a4', 'a5'])
    expect(origins(into.queue)).toBe('cuuuccc')
    // A queued title dropped into the album becomes part of it.
    const out = run(queued, { type: 'REORDER_QUEUE', payload: { fromIndex: 1, toIndex: 4 } })
    expect(ids(out.queue)).toEqual(['a1', 'u2', 'a2', 'a3', 'u1', 'a4', 'a5'])
    expect(origins(out.queue)).toBe('cuccccc')
  })

  it('keeps the origin of the current track and of titles moved into what already played', () => {
    const queued = run(playing(), { type: 'NEXT', payload: { manual: true } }, {
      type: 'ADD_TO_QUEUE',
      payload: { tracks: t('u1') },
    })
    const s = run(queued, { type: 'REORDER_QUEUE', payload: { fromIndex: 2, toIndex: 0 } })
    expect(ids(s.queue)).toEqual(['u1', 'a1', 'a2', 'a3', 'a4', 'a5'])
    expect(s.queue[0]!.queueOrigin).toBe('user')
    expect(s.currentTrack?.id).toBe('a2')
    const moved = moveQueueEntry(queued.queue, queued.queueIndex, 1, 4)
    expect(moved.queue[4]!.id).toBe('a2')
    expect(queueOrigin(moved.queue[4])).toBe('context')
  })

  it('removes from any section without changing the others', () => {
    const queued = run(playing(), { type: 'ADD_TO_QUEUE', payload: { tracks: [t('u1'), t('u2')] } }, {
      type: 'APPEND_AUTOPLAY',
      payload: { tracks: [t('r1'), t('r2')] },
    })
    expect(origins(queued.queue)).toBe('cuuccccaa')
    const s = run(
      queued,
      { type: 'REMOVE_FROM_QUEUE', payload: { index: 1 } },
      { type: 'REMOVE_FROM_QUEUE', payload: { index: 6 } },
    )
    expect(ids(s.queue)).toEqual(['a1', 'u2', 'a2', 'a3', 'a4', 'a5', 'r2'])
    expect(origins(s.queue)).toBe('cucccca')
  })
})

describe('queue origins: shuffle', () => {
  it('shuffles the context only and restores its order when turned off', () => {
    const queued = run(
      playing(),
      { type: 'ADD_TO_QUEUE', payload: { tracks: [t('u1'), t('u2')] } },
      { type: 'APPEND_AUTOPLAY', payload: { tracks: [t('r1')] } },
    )
    for (let i = 0; i < 10; i++) {
      const on = run(queued, { type: 'SET_SHUFFLE', payload: true })
      expect(on.queueIndex).toBe(0)
      expect(ids(on.queue).slice(0, 3)).toEqual(['a1', 'u1', 'u2'])
      expect(on.queue.at(-1)!.id).toBe('r1')
      expect(ids(on.queue).slice(3, 7).sort()).toEqual(['a2', 'a3', 'a4', 'a5'])
      const off = run(on, { type: 'SET_SHUFFLE', payload: false })
      expect(ids(off.queue)).toEqual(ids(queued.queue))
      expect(off.queueIndex).toBe(0)
    }
  })

  it('puts the current context track back in its place and keeps the queued titles next', () => {
    const on = run(playing(), { type: 'SET_SHUFFLE', payload: true }, { type: 'NEXT', payload: { manual: true } })
    const current = on.currentTrack!.id
    const s = run(on, { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } }, { type: 'SET_SHUFFLE', payload: false })
    const at = ids(album).indexOf(current)
    expect(s.queueIndex).toBe(at)
    expect(s.currentTrack?.id).toBe(current)
    expect(ids(s.queue)).toEqual([...ids(album).slice(0, at + 1), 'u1', ...ids(album).slice(at + 1)])
  })

  it('starts a shuffled list with the player shuffle on, so turning it off restores the list order', () => {
    const action = playArtistAction(album, true, albumContext)
    // Drawn before the reducer runs: running it twice gives the same queue.
    expect(ids(playerReducer(INITIAL_PLAYER_STATE, action).queue)).toEqual(
      ids(playerReducer(INITIAL_PLAYER_STATE, action).queue),
    )
    const s = playerReducer(INITIAL_PLAYER_STATE, action)
    expect(s.shuffle).toBe(true)
    expect(s.queueContext).toEqual(albumContext)
    const off = run(s, { type: 'SET_SHUFFLE', payload: false })
    expect(ids(off.queue)).toEqual(ids(album))
    expect(off.currentTrack?.id).toBe(s.currentTrack?.id)
    expect(off.queueIndex).toBe(ids(album).indexOf(s.currentTrack!.id))
  })

  it('plays a list in order, with shuffle off, when a track of it is picked', () => {
    const shuffled = playerReducer(INITIAL_PLAYER_STATE, playArtistAction(album, true))
    const s = run(shuffled, { type: 'PLAY_TRACK', payload: { track: album[2]!, queue: album, queueIndex: 2 } })
    expect(s.shuffle).toBe(false)
    expect(ids(s.queue)).toEqual(ids(album))
  })
})

describe('autoplay and radio', () => {
  it('appends similar tracks not yet queued, and only while the queue would end', () => {
    const s = run(playing(), { type: 'APPEND_AUTOPLAY', payload: { tracks: [t('a2'), t('r1'), t('r1'), t('r2')] } })
    expect(ids(s.queue)).toEqual(['a1', 'a2', 'a3', 'a4', 'a5', 'r1', 'r2'])
    expect(origins(s.queue)).toBe('cccccaa')
    const repeat = run(playing(), { type: 'SET_REPEAT_MODE', payload: 'queue' })
    expect(run(repeat, { type: 'APPEND_AUTOPLAY', payload: { tracks: [t('r1')] } })).toBe(repeat)
    expect(run(INITIAL_PLAYER_STATE, { type: 'APPEND_AUTOPLAY', payload: { tracks: [t('r1')] } })).toBe(
      INITIAL_PLAYER_STATE,
    )
  })

  it('continues into autoplay tracks at the end and replaces them when something new starts', () => {
    let s = run(playing(), { type: 'PLAY_QUEUE_INDEX', payload: { index: 4 } }, {
      type: 'APPEND_AUTOPLAY',
      payload: { tracks: [t('r1')] },
    })
    s = run(s, { type: 'NEXT', payload: { manual: false } })
    expect(s.currentTrack?.id).toBe('r1')
    expect(s.status).toBe('buffering')
    const fresh = run(s, { type: 'PLAY_ALBUM', payload: { tracks: [t('b1'), t('b2')] } })
    expect(origins(fresh.queue)).toBe('cc')
    expect(fresh.queueContext).toBeNull()
  })

  it('wants similar tracks only near the end and when nothing ends playback on purpose', () => {
    const near = { ...run(playing(), { type: 'PLAY_QUEUE_INDEX', payload: { index: 2 } }), status: 'playing' as const }
    expect(wantsAutoplay(near)).toBe(true)
    expect(wantsAutoplay({ ...near, queueIndex: 1, currentTrack: near.queue[1]! })).toBe(false)
    expect(wantsAutoplay({ ...near, autoplay: false })).toBe(false)
    expect(wantsAutoplay(near, false)).toBe(false)
    expect(wantsAutoplay({ ...near, repeatMode: 'queue' })).toBe(false)
    expect(wantsAutoplay({ ...near, repeatMode: 'track' })).toBe(false)
    expect(wantsAutoplay({ ...near, stopAfter: 'track' })).toBe(false)
    expect(wantsAutoplay({ ...near, stopAfter: 'album' })).toBe(false)
    expect(wantsAutoplay({ ...near, sleepTimer: '30' })).toBe(false)
    expect(wantsAutoplay({ ...near, sleepTimer: 'end_of_album' })).toBe(false)
    expect(wantsAutoplay({ ...near, status: 'paused' })).toBe(false)
    expect(wantsAutoplay({ ...near, currentTrack: { ...near.currentTrack!, offline_blob_key: 'k' } })).toBe(false)
  })

  it('seeds autoplay with the last track the user chose', () => {
    const s = run(playing(), { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } }, {
      type: 'APPEND_AUTOPLAY',
      payload: { tracks: [t('r1'), t('r2')] },
    })
    expect(autoplaySeed(s.queue)?.id).toBe('a5')
  })

  it('starts a radio from the playing track without interrupting it', () => {
    const before = { ...playing(), status: 'playing' as const, currentTime: 42 }
    const s = run(before, {
      type: 'START_RADIO',
      payload: { tracks: [t('r1'), t('a1'), t('r2')], seed: album[0]!, context: radioContext({ seed: album[0]! }) },
    })
    expect(ids(s.queue)).toEqual(['a1', 'r1', 'r2'])
    expect(s.queueIndex).toBe(0)
    expect(s.status).toBe('playing')
    expect(s.currentTime).toBe(42)
    expect(s.history).toBe(before.history)
    expect(s.queueContext).toEqual({ kind: 'radio', id: 'a1', title: 'Radio: A1' })
  })

  it('starts a radio from another track or a genre with that first', () => {
    const fromSeed = run(playing(), {
      type: 'START_RADIO',
      payload: { tracks: [t('r1')], seed: t('s1'), context: radioContext({ seed: t('s1') }) },
    })
    expect(ids(fromSeed.queue)).toEqual(['s1', 'r1'])
    expect(fromSeed.status).toBe('buffering')
    const genre = run(playing(), {
      type: 'START_RADIO',
      payload: { tracks: [t('r1'), t('r2')], context: radioContext({ genre: 'Indie' }) },
    })
    expect(ids(genre.queue)).toEqual(['r1', 'r2'])
    expect(genre.queueContext?.title).toBe('Radio: Indie')
    const nothing = playing()
    expect(run(nothing, { type: 'START_RADIO', payload: { tracks: [], context: radioContext({}) } })).toBe(nothing)
  })
})

describe('clearing and undo', () => {
  it('restores the cleared queue as it was, but never over something started since', () => {
    const queued = { ...run(playing(), { type: 'ADD_TO_QUEUE', payload: { tracks: t('u1') } }), status: 'playing' as const, currentTime: 12 }
    const snapshot = {
      queue: queued.queue,
      originalQueue: queued.originalQueue,
      queueIndex: queued.queueIndex,
      currentTrack: queued.currentTrack,
      queueContext: queued.queueContext,
      shuffle: queued.shuffle,
      currentTime: queued.currentTime,
      status: queued.status,
    }
    const cleared = run(queued, { type: 'CLEAR_QUEUE' })
    expect(cleared.queueContext).toBeNull()
    const restored = run(cleared, { type: 'RESTORE_QUEUE', payload: snapshot })
    expect(ids(restored.queue)).toEqual(ids(queued.queue))
    expect(origins(restored.queue)).toBe(origins(queued.queue))
    expect(restored.queueContext).toEqual(albumContext)
    expect(restored.currentTrack?.id).toBe('a1')
    expect(restored.currentTime).toBe(12)
    expect(restored.status).toBe('buffering')
    const other = run(cleared, { type: 'PLAY_ALBUM', payload: { tracks: [t('b1')] } })
    expect(run(other, { type: 'RESTORE_QUEUE', payload: snapshot })).toBe(other)
    const paused = run(cleared, { type: 'RESTORE_QUEUE', payload: { ...snapshot, status: 'paused' } })
    expect(paused.status).toBe('paused')
  })
})

describe('persisted queues', () => {
  afterEach(() => localStorage.clear())

  it('reads a queue saved before origins existed as context, and drops unknown marks', () => {
    localStorage.setItem(
      'ytmdl.player.v2.u.state',
      JSON.stringify({
        schemaVersion: 2,
        queue: [t('a1'), { ...t('a2'), queueOrigin: 'user' }, { ...t('a3'), queueOrigin: 'bogus' }, { ...t('r1'), queueOrigin: 'autoplay' }],
        originalQueue: [t('a1')],
        queueIndex: 0,
      }),
    )
    const saved = loadPlayerState('u')!
    expect(origins(saved.queue!)).toBe('cuca')
    expect(saved.queue![2]).not.toHaveProperty('queueOrigin')
    expect(saved.queueContext).toBeUndefined()
    const s = run(INITIAL_PLAYER_STATE, { type: 'RESTORE_STATE', payload: saved })
    expect(s.queueContext).toBeNull()
    expect(s.autoplay).toBe(true)
    expect(s.currentTrack?.id).toBe('a1')
  })

  it('keeps a valid context and the autoplay choice, and refuses a malformed context', () => {
    localStorage.setItem(
      'ytmdl.player.v2.u.state',
      JSON.stringify({ queue: [t('a1')], queueIndex: 0, queueContext: { kind: 'playlist', id: 'p', title: 'Liste' }, autoplay: false }),
    )
    expect(loadPlayerState('u')).toMatchObject({ queueContext: { kind: 'playlist', id: 'p', title: 'Liste' }, autoplay: false })
    localStorage.setItem('ytmdl.player.v2.u.state', JSON.stringify({ queueContext: { kind: 'evil', title: 1 } }))
    expect(loadPlayerState('u')?.queueContext).toBeNull()
  })
})
