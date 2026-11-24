import { expect, test } from 'bun:test'
import { ListeningTracker, movesOnToAnotherTrack } from './listeningTracker'
test('history ignores pauses, seeks, hidden-tab gaps and duplicate callbacks', () => {
  const t = new ListeningTracker()
  expect(t.tick('one', true, 0, 120)).toBe(false)
  expect(t.tick('one', false, 100000, 120)).toBe(false)
  expect(t.tick('one', true, 200000, 120)).toBe(false)
  for (let n = 1; n < 30; n++) expect(t.tick('one', true, 200000 + n * 1000, 120)).toBe(false)
  expect(t.tick('one', true, 230000, 120)).toBe(true)
  expect(t.tick('one', true, 231000, 120)).toBe(false)
  expect(t.tick('two', true, 232000, 4)).toBe(false)
  expect(t.tick('two', true, 233000, 4)).toBe(false)
  expect(t.tick('two', true, 234000, 4)).toBe(true)
  t.reset()
  expect(t.tick('two', true, 235000, 4)).toBe(false)
})

test('a skip needs 2 s of listening, stays below the play threshold and is reported once per start', () => {
  const t = new ListeningTracker()
  t.tick('one', true, 0, 200)
  t.tick('one', true, 1000, 200)
  expect(t.takeSkip('one')).toBe(false) // one second is not a judgement
  t.tick('one', true, 2000, 200)
  expect(t.takeSkip('other')).toBe(false) // only the track being tracked
  expect(t.takeSkip('one')).toBe(true)
  expect(t.takeSkip('one')).toBe(false) // at most once per track start
  // A new start of the track can be skipped again.
  t.tick('two', true, 3000, 200)
  t.tick('one', true, 4000, 200)
  for (let n = 1; n <= 3; n++) t.tick('one', true, 4000 + n * 1000, 200)
  expect(t.takeSkip('one')).toBe(true)
  // Once the track counted as a play, moving on is no skip.
  t.reset()
  for (let n = 0; n <= 30; n++) t.tick('one', true, n * 1000, 200)
  expect(t.takeSkip('one')).toBe(false)
  // Paused time does not count; a reset after a natural end leaves nothing to skip.
  t.reset()
  t.tick('one', true, 0, 200)
  t.tick('one', false, 1000, 200)
  t.tick('one', true, 60000, 200)
  expect(t.takeSkip('one')).toBe(false)
  t.tick('one', true, 61000, 200)
  t.reset()
  expect(t.takeSkip('one')).toBe(false)
})

test('only moving on to another playable track can be a skip', () => {
  const a = { id: 'a' },
    b = { id: 'b' },
    playing = { currentTrack: a, status: 'playing', error: null }
  expect(movesOnToAnotherTrack(playing, { currentTrack: b })).toBe(true)
  expect(movesOnToAnotherTrack(playing, { currentTrack: a })).toBe(false) // restart
  expect(movesOnToAnotherTrack(playing, { currentTrack: null })).toBe(false) // stop, cleared queue
  expect(movesOnToAnotherTrack({ ...playing, status: 'error' }, { currentTrack: b })).toBe(false)
  expect(movesOnToAnotherTrack({ ...playing, error: 'decode' }, { currentTrack: b })).toBe(false)
  expect(movesOnToAnotherTrack({ ...playing, currentTrack: { id: 'a', offline_blob_key: 'k' } }, { currentTrack: b })).toBe(false)
  expect(movesOnToAnotherTrack({ ...playing, currentTrack: null }, { currentTrack: b })).toBe(false)
})
