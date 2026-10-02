import { describe, expect, it } from 'bun:test'
import { deletionSelection, duplicateSwipe } from './duplicate-review'

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
