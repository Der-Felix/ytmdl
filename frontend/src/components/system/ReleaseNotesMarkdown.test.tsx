import { afterEach, describe, expect, it } from 'bun:test'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'

import { ReleaseNotesMarkdown } from './ReleaseNotesMarkdown'

describe('ReleaseNotesMarkdown', () => {
  afterEach(() => {
    cleanup()
  })
  it('renders Markdown headings (h1, h2, h3) with appropriate hierarchy and classes', () => {
    const markdown = `# Title Heading
## Section Heading
### Subsection Heading`

    render(<ReleaseNotesMarkdown content={markdown} />)

    const h1 = screen.getByText('Title Heading')
    expect(h1.tagName.toLowerCase()).toBe('h3')

    const h2 = screen.getByText('Section Heading')
    expect(h2.tagName.toLowerCase()).toBe('h4')

    const h3 = screen.getByText('Subsection Heading')
    expect(h3.tagName.toLowerCase()).toBe('h5')
  })

  it('renders bullet lists and nested sub-items', () => {
    const markdown = `- Main item 1
- Main item 2
  - Sub item 2.1
- Main item 3`

    const { container } = render(<ReleaseNotesMarkdown content={markdown} />)

    expect(screen.getByText('Main item 1')).toBeDefined()
    expect(screen.getByText('Main item 2')).toBeDefined()
    expect(screen.getByText('Sub item 2.1')).toBeDefined()
    expect(screen.getByText('Main item 3')).toBeDefined()

    const listElements = container.querySelectorAll('ul')
    expect(listElements.length).toBeGreaterThanOrEqual(1)

    const subItem = screen.getByText('Sub item 2.1')
    expect(subItem.className).toContain('list-circle')
  })

  it('renders bold text correctly', () => {
    const markdown = 'This contains **strong highlighted text** inside a sentence.'

    const { container } = render(<ReleaseNotesMarkdown content={markdown} />)

    const strong = container.querySelector('strong')
    expect(strong).not.toBeNull()
    expect(strong?.textContent).toBe('strong highlighted text')
  })

  it('renders inline code with monospace styling', () => {
    const markdown = 'Run `ytmdlctl update --dry-run` to test first.'

    const { container } = render(<ReleaseNotesMarkdown content={markdown} />)

    const code = container.querySelector('code')
    expect(code).not.toBeNull()
    expect(code?.textContent).toBe('ytmdlctl update --dry-run')
    expect(code?.className).toContain('font-mono')
  })

  it('renders fenced code blocks', () => {
    const markdown = '```bash\nytmdlctl backup\nytmdlctl status\n```'

    const { container } = render(<ReleaseNotesMarkdown content={markdown} />)

    const pre = container.querySelector('pre')
    expect(pre).not.toBeNull()
    const code = pre?.querySelector('code')
    expect(code?.textContent).toContain('ytmdlctl backup\nytmdlctl status')
  })

  it('renders hyperlinks with external security attributes', () => {
    const markdown = 'See documentation at [Release Docs](https://github.com/Der-Felix/ytmdl/releases) or direct link https://example.com/info.'

    render(<ReleaseNotesMarkdown content={markdown} />)

    const namedLink = screen.getByRole('link', { name: 'Release Docs' })
    expect(namedLink).toBeDefined()
    expect(namedLink.getAttribute('href')).toBe('https://github.com/Der-Felix/ytmdl/releases')
    expect(namedLink.getAttribute('target')).toBe('_blank')
    expect(namedLink.getAttribute('rel')).toBe('noopener noreferrer')

    const bareLink = screen.getByRole('link', { name: 'https://example.com/info' })
    expect(bareLink).toBeDefined()
    expect(bareLink.getAttribute('href')).toBe('https://example.com/info')
    expect(bareLink.getAttribute('target')).toBe('_blank')
    expect(bareLink.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('strictly blocks raw HTML execution and prevents injection', () => {
    const malicious = '<script>alert(1)</script><img src=x onerror="alert(2)" /><div class="injected-payload">danger</div>'

    const { container } = render(<ReleaseNotesMarkdown content={malicious} />)

    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('.injected-payload')).toBeNull()

    // Ensure raw text is rendered safely as string characters
    expect(container.textContent).toContain('<script>alert(1)</script>')
    expect(container.textContent).toContain('<img src=x onerror="alert(2)" />')
  })

  it('rejects malicious URI schemes (javascript:, data:, file:) and renders them safely as text', () => {
    const maliciousLinks = 'Try [click me](javascript:alert(1)) or [data link](data:text/html,<script>alert(1)</script>) or [file link](file:///etc/passwd).'

    const { container } = render(<ReleaseNotesMarkdown content={maliciousLinks} />)

    // None of the malicious schemes should produce an active <a> link
    expect(container.querySelector('a')).toBeNull()
    expect(container.textContent).toContain('javascript:alert(1)')
    expect(container.textContent).toContain('data:text/html')
    expect(container.textContent).toContain('file:///etc/passwd')
  })

  it('returns null on empty release notes', () => {
    const { container } = render(<ReleaseNotesMarkdown content="" />)
    expect(container.firstChild).toBeNull()
  })

  it('returns null on whitespace-only release notes', () => {
    const { container } = render(<ReleaseNotesMarkdown content={'   \n\n  '} />)
    expect(container.firstChild).toBeNull()
  })

  it('handles long release body with toggle button ("Mehr anzeigen" / "Weniger anzeigen")', () => {
    const longNotes = Array.from({ length: 14 }, (_, i) => `- Change item #${i + 1} with detailed explanation`).join('\n')

    render(<ReleaseNotesMarkdown content={longNotes} />)

    const toggleBtn = screen.getByRole('button', { name: /Mehr anzeigen/i })
    expect(toggleBtn).toBeDefined()
    expect(screen.getByTestId('fade-overlay')).toBeDefined()

    // Expand
    fireEvent.click(toggleBtn)
    expect(screen.getByRole('button', { name: /Weniger anzeigen/i })).toBeDefined()
    expect(screen.queryByTestId('fade-overlay')).toBeNull()

    // Collapse
    fireEvent.click(screen.getByRole('button', { name: /Weniger anzeigen/i }))
    expect(screen.getByRole('button', { name: /Mehr anzeigen/i })).toBeDefined()
    expect(screen.getByTestId('fade-overlay')).toBeDefined()
  })

  it('does not display toggle button for short release notes', () => {
    const shortNotes = '- Quick fix for styling\n- Updated docs'

    render(<ReleaseNotesMarkdown content={shortNotes} />)

    expect(screen.queryByRole('button', { name: /Mehr anzeigen/i })).toBeNull()
    expect(screen.queryByTestId('fade-overlay')).toBeNull()
  })

  it('renders the expected v0.18.0-style release body flawlessly', () => {
    const v0180Notes = `# YTMDL v0.18.0

## Highlights

- Added live Download Queue ETA based on measured track throughput
- Added active-processing preview
- Added Next-Up queue preview
- Added live queue summary updates

## Queue improvements

- Paused jobs are excluded from active ETA calculations
- Next-Up follows the same starvation-aware priority ordering as the dispatcher
- ETA avoids false precision when insufficient history is available

## Update

Existing v0.17.4 installations can update with:

\`ytmdlctl update\`

Database schema remains version 9; no migration is required.

**Full Changelog:** [v0.17.4...v0.18.0](https://github.com/Der-Felix/ytmdl/compare/v0.17.4...v0.18.0)`

    const { container } = render(<ReleaseNotesMarkdown content={v0180Notes} />)

    // Heading 1
    expect(screen.getByText('YTMDL v0.18.0')).toBeDefined()

    // Section headings
    expect(screen.getByText('Highlights')).toBeDefined()
    expect(screen.getByText('Queue improvements')).toBeDefined()
    expect(screen.getByText('Update')).toBeDefined()

    // Bullets
    expect(screen.getByText(/Added live Download Queue ETA/i)).toBeDefined()
    expect(screen.getByText(/Paused jobs are excluded/i)).toBeDefined()

    // Code snippet
    const code = container.querySelector('code')
    expect(code?.textContent).toBe('ytmdlctl update')

    // Bold text
    const bold = container.querySelector('strong')
    expect(bold?.textContent).toBe('Full Changelog:')

    // Compare link
    const compareLink = screen.getByRole('link', { name: 'v0.17.4...v0.18.0' })
    expect(compareLink).toBeDefined()
    expect(compareLink.getAttribute('href')).toBe('https://github.com/Der-Felix/ytmdl/compare/v0.17.4...v0.18.0')
    expect(compareLink.getAttribute('target')).toBe('_blank')
    expect(compareLink.getAttribute('rel')).toBe('noopener noreferrer')
  })
})
