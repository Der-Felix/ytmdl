import { expect, test } from 'bun:test'
import { ListeningTracker } from './listeningTracker'
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
