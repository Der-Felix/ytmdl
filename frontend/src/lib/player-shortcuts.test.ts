import { afterEach, describe, expect, it } from 'bun:test'

import { playerShortcutFor } from './player-shortcuts'

function press(key: string, init: Partial<KeyboardEvent> & { target?: EventTarget | null } = {}) {
  return playerShortcutFor({
    key,
    shiftKey: false,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    repeat: false,
    defaultPrevented: false,
    target: document.body,
    ...init,
  })
}

function mount(html: string): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = html
  document.body.appendChild(host)
  return host
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('playerShortcutFor', () => {
  it('maps the player keys on the page', () => {
    expect(press(' ')).toBe('togglePlay')
    expect(press('ArrowLeft')).toBe('seekBackward')
    expect(press('ArrowRight')).toBe('seekForward')
    expect(press('ArrowLeft', { shiftKey: true })).toBe('previous')
    expect(press('ArrowRight', { shiftKey: true })).toBe('next')
    expect(press('m')).toBe('toggleMute')
    expect(press('S')).toBe('toggleShuffle')
    expect(press('r')).toBe('cycleRepeat')
  })

  it('leaves plain ↑/↓ to scrolling and moves the volume with Shift', () => {
    expect(press('ArrowUp')).toBeNull()
    expect(press('ArrowDown')).toBeNull()
    expect(press('ArrowUp', { shiftKey: true })).toBe('volumeUp')
    expect(press('ArrowDown', { shiftKey: true })).toBe('volumeDown')
  })

  it('leaves Space to a focused button and arrows to menus, tabs and sliders', () => {
    const host = mount(`
      <button id="b"><span id="inner">Play</span></button>
      <a id="a" href="/x">x</a>
      <div role="menu"><div id="mi" role="menuitem">Eins</div></div>
      <div role="tablist"><button id="tab" role="tab">Lyrics</button></div>
      <div id="slider" role="slider" tabindex="0"></div>
      <div cmdk-root><div id="cmdk">x</div></div>
      <div role="dialog"><p id="dlg">Text</p></div>
    `)
    const el = (id: string) => host.querySelector(`#${id}`)
    expect(press(' ', { target: el('b') })).toBeNull()
    expect(press(' ', { target: el('inner') })).toBeNull()
    expect(press(' ', { target: el('a') })).toBeNull()
    expect(press('ArrowRight', { target: el('mi') })).toBeNull()
    expect(press('ArrowRight', { target: el('tab') })).toBeNull()
    expect(press('ArrowLeft', { target: el('slider') })).toBeNull()
    expect(press('m', { target: el('cmdk') })).toBeNull()
    expect(press(' ', { target: el('dlg') })).toBeNull()
  })

  it('leaves every key to text fields and editable content', () => {
    const host = mount(`
      <input id="i" />
      <input id="range" type="range" />
      <textarea id="t"></textarea>
      <select id="s"><option>1</option></select>
      <div id="ce" contenteditable="true"><span id="ce-inner">x</span></div>
    `)
    for (const id of ['i', 'range', 't', 's', 'ce', 'ce-inner']) {
      const target = host.querySelector(`#${id}`)
      expect(press(' ', { target })).toBeNull()
      expect(press('m', { target })).toBeNull()
      expect(press('ArrowLeft', { target })).toBeNull()
    }
  })

  it('ignores held Space, extra modifiers and handled events', () => {
    expect(press(' ', { repeat: true })).toBeNull()
    expect(press(' ', { shiftKey: true })).toBeNull()
    expect(press('m', { shiftKey: true })).toBeNull()
    expect(press('s', { ctrlKey: true })).toBeNull()
    expect(press('r', { metaKey: true })).toBeNull()
    expect(press('ArrowLeft', { altKey: true })).toBeNull()
    expect(press(' ', { defaultPrevented: true })).toBeNull()
    expect(press('k', { ctrlKey: true })).toBeNull()
  })

  it('opens the overview with "?" whatever Shift does on the layout', () => {
    expect(press('?')).toBe('showHelp')
    expect(press('?', { shiftKey: true })).toBe('showHelp')
    expect(press('ß', { shiftKey: false })).toBeNull()
  })
})
