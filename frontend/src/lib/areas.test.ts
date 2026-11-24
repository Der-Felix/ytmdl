import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { renderHook } from '@testing-library/react'

import { AREA_NAV } from '@/components/layout/navigation'
import { AREA_START, DEFAULT_AREA, ROUTE_AREAS, areaFor, loadArea, resetStoredArea, saveArea, useArea } from './areas'
import { matchRoute } from './router'
import type { Route } from './router'

/** Every route name in the Route union, read from the router's source. */
function routeNames(): string[] {
  const source = readFileSync(new URL('./router.tsx', import.meta.url), 'utf8')
  const union = source.slice(source.indexOf('export type Route ='), source.indexOf('export function matchRoute'))
  return [...union.matchAll(/name: '(\w+)'/g)].map((match) => match[1]!)
}

function routeAt(href: string): Route {
  const [pathname = '/', search = ''] = href.split('?')
  return matchRoute({ pathname, params: new URLSearchParams(search), href })
}

beforeEach(() => resetStoredArea())
afterEach(() => resetStoredArea())

describe('route areas', () => {
  it('assigns an area to every route, so a new route cannot be forgotten', () => {
    const names = routeNames()
    expect(names.length).toBeGreaterThan(20)
    expect(Object.keys(ROUTE_AREAS).sort()).toEqual([...names].sort())
  })

  it('puts listening, loading and the neutral pages where the switch says', () => {
    const listen = ['/listen', '/player', '/mixes', '/mixes/genre-1', '/offline', '/library', '/library/artists/a', '/library/releases/r', '/playlists', '/playlists/p', '/favorites']
    const download = ['/search', '/dashboard', '/discover?q=x', '/artists/a', '/releases/r', '/downloads', '/downloads/j', '/subscriptions']
    const neutral = ['/', '/login', '/profile', '/users', '/settings/server', '/nowhere']
    for (const href of listen) expect(ROUTE_AREAS[routeAt(href).name]).toBe('listen')
    for (const href of download) expect(ROUTE_AREAS[routeAt(href).name]).toBe('download')
    for (const href of neutral) expect(ROUTE_AREAS[routeAt(href).name]).toBe('neutral')
  })

  it('starts each area on its own page, which belongs to that area', () => {
    expect(ROUTE_AREAS[routeAt(AREA_START.listen).name]).toBe('listen')
    expect(ROUTE_AREAS[routeAt(AREA_START.download).name]).toBe('download')
  })

  it('lists in the sidebar only entries that lead into the same area', () => {
    for (const area of ['listen', 'download'] as const) {
      for (const item of AREA_NAV[area]) {
        expect(ROUTE_AREAS[routeAt(item.href).name]).toBe(area)
        for (const name of item.matches) expect(ROUTE_AREAS[name]).toBe(area)
      }
    }
  })
})

describe('stored area', () => {
  it('defaults to listening', () => {
    expect(loadArea()).toBe(DEFAULT_AREA)
    expect(DEFAULT_AREA).toBe('listen')
  })

  it('remembers the last area in this browser', () => {
    saveArea('download')
    expect(localStorage.getItem('ytmdl.area')).toBe('download')
    expect(loadArea()).toBe('download')
  })

  it('ignores a stored value that is not an area', () => {
    localStorage.setItem('ytmdl.area', 'garbage')
    expect(loadArea()).toBe('listen')
  })

  it('keeps the choice in memory when storage refuses access', () => {
    const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')!
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new Error('denied')
      },
    })
    try {
      expect(loadArea()).toBe('listen')
      saveArea('download')
      expect(loadArea()).toBe('download')
    } finally {
      Object.defineProperty(globalThis, 'localStorage', original)
    }
  })

  it('lets a deep link win over the stored area', () => {
    saveArea('listen')
    expect(areaFor(routeAt('/downloads'))).toBe('download')
    saveArea('download')
    expect(areaFor(routeAt('/favorites'))).toBe('listen')
  })

  it('shows the stored area on a neutral page', () => {
    saveArea('download')
    expect(areaFor(routeAt('/profile'))).toBe('download')
    saveArea('listen')
    expect(areaFor(routeAt('/settings/server'))).toBe('listen')
  })

  it('stores the area of each visited page, but not of a neutral one', () => {
    const { result, rerender } = renderHook(({ route }) => useArea(route), {
      initialProps: { route: routeAt('/downloads') },
    })
    expect(result.current).toBe('download')
    expect(loadArea()).toBe('download')

    rerender({ route: routeAt('/profile') })
    expect(result.current).toBe('download')
    expect(loadArea()).toBe('download')

    rerender({ route: routeAt('/library') })
    expect(result.current).toBe('listen')
    expect(loadArea()).toBe('listen')
  })
})
