import { describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'

import { QueueDrawer } from './QueueDrawer'
import type { LibraryTrack } from '@/types/api'

const dummyTrack1: LibraryTrack = {
  id: 'track-1',
  title: 'Track One',
  artists: ['Artist A'],
  album: 'Album 1',
  album_artist: 'Artist A',
  release_id: 'rel-1',
  track_number: 1,
  track_total: 10,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 180000,
  year: 2024,
  source_provider: 'youtube',
  source_id: 'src-1',
  source_url: 'https://...',
  created_at: new Date().toISOString(),
}

const dummyTrack2: LibraryTrack = {
  ...dummyTrack1,
  id: 'track-2',
  title: 'Track Two',
  track_number: 2,
  duration_ms: 240000,
}

const dummyTrack3: LibraryTrack = {
  ...dummyTrack1,
  id: 'track-3',
  title: 'Track Three',
  track_number: 3,
  duration_ms: 200000,
}

// Mock usePlayer hook
const mockPlayQueueIndex = mock(() => {})
const mockRemoveFromQueue = mock(() => {})
const mockReorderQueue = mock(() => {})
const mockClearUpcomingQueue = mock(() => {})
const mockClearQueue = mock(() => {})

let mockState = {
  queue: [dummyTrack1, dummyTrack2, dummyTrack3],
  queueIndex: 0,
  currentTrack: dummyTrack1,
  status: 'playing',
  playQueueIndex: mockPlayQueueIndex,
  removeFromQueue: mockRemoveFromQueue,
  reorderQueue: mockReorderQueue,
  clearUpcomingQueue: mockClearUpcomingQueue,
  clearQueue: mockClearQueue,
}

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => mockState,
}))

describe('QueueDrawer', () => {
  it('renders queue items with active track indicator', () => {
    mockState = {
      queue: [dummyTrack1, dummyTrack2, dummyTrack3],
      queueIndex: 0,
      currentTrack: dummyTrack1,
      status: 'playing',
      playQueueIndex: mockPlayQueueIndex,
      removeFromQueue: mockRemoveFromQueue,
      reorderQueue: mockReorderQueue,
      clearUpcomingQueue: mockClearUpcomingQueue,
      clearQueue: mockClearQueue,
    }

    render(<QueueDrawer open={true} onOpenChange={() => {}} />)

    expect(screen.getByText('Warteschlange')).toBeDefined()
    expect(screen.getByText('3 Titel')).toBeDefined()
    expect(screen.getByText('Track One')).toBeDefined()
    expect(screen.getByText('Track Two')).toBeDefined()
    expect(screen.getByText('Track Three')).toBeDefined()

    // Track One is active track
    const activeItem = screen.getByText('Track One').closest('[aria-current="true"]')
    expect(activeItem).toBeDefined()
  })

  it('handles clicking track to play via playQueueIndex', () => {
    mockPlayQueueIndex.mockClear()
    render(<QueueDrawer open={true} onOpenChange={() => {}} />)

    const track2Title = screen.getByText('Track Two')
    fireEvent.click(track2Title)

    expect(mockPlayQueueIndex).toHaveBeenCalledWith(1)
  })

  it('handles remove track action', () => {
    mockRemoveFromQueue.mockClear()
    render(<QueueDrawer open={true} onOpenChange={() => {}} />)

    const removeButtons = screen.getAllByTitle('Aus Warteschlange entfernen')
    expect(removeButtons.length).toBe(3)
    fireEvent.click(removeButtons[1])

    expect(mockRemoveFromQueue).toHaveBeenCalledWith(1)
  })

  it('handles accessible move up and down actions', () => {
    mockReorderQueue.mockClear()
    render(<QueueDrawer open={true} onOpenChange={() => {}} />)

    // Move Down on first track
    const moveDownButtons = screen.getAllByTitle('Nach unten verschieben')
    fireEvent.click(moveDownButtons[0])
    expect(mockReorderQueue).toHaveBeenCalledWith(0, 1)

    // Move Up on second track
    mockReorderQueue.mockClear()
    const moveUpButtons = screen.getAllByTitle('Nach oben verschieben')
    fireEvent.click(moveUpButtons[1])
    expect(mockReorderQueue).toHaveBeenCalledWith(1, 0)
  })

  it('handles clear upcoming and clear queue actions', () => {
    mockClearUpcomingQueue.mockClear()
    mockClearQueue.mockClear()
    render(<QueueDrawer open={true} onOpenChange={() => {}} />)

    const clearUpcomingBtn = screen.getByText('Nächste leeren')
    fireEvent.click(clearUpcomingBtn)
    expect(mockClearUpcomingQueue).toHaveBeenCalledTimes(1)

    const clearBtn = screen.getByText('Leeren')
    fireEvent.click(clearBtn)
    expect(mockClearQueue).toHaveBeenCalledTimes(1)
  })

  it('renders empty state when queue is empty', () => {
    mockState = {
      queue: [],
      queueIndex: -1,
      currentTrack: null,
      status: 'idle',
      playQueueIndex: mockPlayQueueIndex,
      removeFromQueue: mockRemoveFromQueue,
      reorderQueue: mockReorderQueue,
      clearUpcomingQueue: mockClearUpcomingQueue,
      clearQueue: mockClearQueue,
    }

    render(<QueueDrawer open={true} onOpenChange={() => {}} />)

    expect(screen.getByText('Die Warteschlange ist leer')).toBeDefined()
  })
})
