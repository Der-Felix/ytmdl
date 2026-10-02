import { describe, expect, it } from 'bun:test'
import {
  deletionSelection,
  duplicateSwipe,
  readDuplicateConfirmation,
  writeDuplicateConfirmation,
} from './duplicate-review'

describe('duplicate review gestures', () => {
  it('ignores scroll, taps, short and diagonal gestures', () => {
    for (const [dx, dy] of [
      [0, 140],
      [89, 0],
      [-40, 0],
      [100, 100],
      [-100, 110],
    ])
      expect(duplicateSwipe(dx!, dy!)).toBeNull()
    expect(duplicateSwipe(-100, 5)).toBe('left')
    expect(duplicateSwipe(120, -15)).toBe('right')
  })
  it('keeps winner, outsiders and repeated IDs out of the confirmed deletion request', () => {
    expect(
      deletionSelection(
        'winner',
        ['winner', 'loser', 'another'],
        ['winner', 'outside', 'loser', 'loser'],
      ),
    ).toEqual(['loser'])
    expect(deletionSelection('winner', ['winner', 'loser'], [])).toEqual([])
  })
})

describe('confirmation preference isolation', () => {
  it('defaults to confirmation and remembers direct mode only for the same account', () => {
    localStorage.clear()
    expect(readDuplicateConfirmation('first')).toBe(true)
    writeDuplicateConfirmation('first', false)
    expect(readDuplicateConfirmation('first')).toBe(false)
    expect(readDuplicateConfirmation('second')).toBe(true)
    localStorage.setItem('ytmdl.duplicate-review.second.confirmation', 'corrupt')
    expect(readDuplicateConfirmation('second')).toBe(true)
    writeDuplicateConfirmation('first', true)
    expect(readDuplicateConfirmation('first')).toBe(true)
    localStorage.clear()
  })
})
