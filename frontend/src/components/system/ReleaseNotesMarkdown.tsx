import React, { useState } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface ReleaseNotesMarkdownProps {
  content: string
  defaultExpanded?: boolean
}

type InlineToken =
  | { type: 'text'; value: string }
  | { type: 'bold'; value: string }
  | { type: 'code'; value: string }
  | { type: 'link'; text: string; url: string }

function parseInlineTokens(text: string): InlineToken[] {
  const tokens: InlineToken[] = []
  // Matches:
  // 1 & 2: [text](url)
  // 3: `code`
  // 4: **bold**
  // 5: bare https:// or http:// URL
  const pattern = /(?:\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)|`([^`]+)`|\*\*([^*]+)\*\*|(https?:\/\/[^\s<]+))/g

  let lastIndex = 0
  let match: RegExpExecArray | null

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      tokens.push({ type: 'text', value: text.slice(lastIndex, match.index) })
    }

    if (match[1] !== undefined && match[2] !== undefined) {
      tokens.push({ type: 'link', text: match[1], url: match[2] })
    } else if (match[3] !== undefined) {
      tokens.push({ type: 'code', value: match[3] })
    } else if (match[4] !== undefined) {
      tokens.push({ type: 'bold', value: match[4] })
    } else if (match[5] !== undefined) {
      let url = match[5]
      let trailingPunct = ''
      while (/[.,;:!?)\]]$/.test(url)) {
        trailingPunct = url.slice(-1) + trailingPunct
        url = url.slice(0, -1)
      }
      tokens.push({ type: 'link', text: url, url })
      if (trailingPunct) {
        tokens.push({ type: 'text', value: trailingPunct })
      }
    }

    lastIndex = pattern.lastIndex
  }

  if (lastIndex < text.length) {
    tokens.push({ type: 'text', value: text.slice(lastIndex) })
  }

  return tokens
}

function renderInline(tokens: InlineToken[], keyPrefix: string): React.ReactNode[] {
  return tokens.map((token, index) => {
    const key = `${keyPrefix}-${index}`
    switch (token.type) {
      case 'bold':
        return (
          <strong key={key} className="font-semibold text-foreground">
            {token.value}
          </strong>
        )
      case 'code':
        return (
          <code
            key={key}
            className="rounded bg-muted/60 px-1.5 py-0.5 font-mono text-[11px] text-foreground border border-border/40"
          >
            {token.value}
          </code>
        )
      case 'link':
        if (!/^https?:\/\//i.test(token.url)) {
          return <React.Fragment key={key}>{token.text}</React.Fragment>
        }
        return (
          <a
            key={key}
            href={token.url}
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-primary hover:underline underline-offset-2 break-all"
          >
            {token.text}
          </a>
        )
      case 'text':
      default:
        return <React.Fragment key={key}>{token.value}</React.Fragment>
    }
  })
}

interface BlockItem {
  type: 'h1' | 'h2' | 'h3' | 'paragraph' | 'codeblock' | 'list'
  content?: string
  lines?: string[]
  listItems?: { indent: number; text: string }[]
}

function parseBlocks(markdown: string): BlockItem[] {
  const lines = markdown.replace(/\r\n/g, '\n').split('\n')
  const blocks: BlockItem[] = []

  let inCodeBlock = false
  let codeLines: string[] = []

  let currentList: { indent: number; text: string }[] = []
  let currentParagraph: string[] = []

  function flushList() {
    if (currentList.length > 0) {
      blocks.push({ type: 'list', listItems: currentList })
      currentList = []
    }
  }

  function flushParagraph() {
    if (currentParagraph.length > 0) {
      blocks.push({ type: 'paragraph', content: currentParagraph.join(' ') })
      currentParagraph = []
    }
  }

  for (const line of lines) {
    // Fenced code block handling
    if (line.trim().startsWith('```')) {
      if (inCodeBlock) {
        blocks.push({ type: 'codeblock', lines: codeLines })
        codeLines = []
        inCodeBlock = false
      } else {
        flushParagraph()
        flushList()
        inCodeBlock = true
        codeLines = []
      }
      continue
    }

    if (inCodeBlock) {
      codeLines.push(line)
      continue
    }

    // Blank line
    if (line.trim() === '') {
      flushParagraph()
      flushList()
      continue
    }

    // Headings
    if (line.startsWith('# ')) {
      flushParagraph()
      flushList()
      blocks.push({ type: 'h1', content: line.slice(2).trim() })
      continue
    }
    if (line.startsWith('## ')) {
      flushParagraph()
      flushList()
      blocks.push({ type: 'h2', content: line.slice(3).trim() })
      continue
    }
    if (line.startsWith('### ')) {
      flushParagraph()
      flushList()
      blocks.push({ type: 'h3', content: line.slice(4).trim() })
      continue
    }

    // Bullet list items
    const listMatch = line.match(/^(\s*)[-*]\s+(.*)$/)
    if (listMatch) {
      flushParagraph()
      const indent = listMatch[1]?.length ?? 0
      const text = listMatch[2] ?? ''
      currentList.push({ indent, text })
      continue
    }

    // If we were in a list and this line doesn't match list item but has indent
    if (currentList.length > 0 && /^\s{2,}/.test(line)) {
      // Continuation of previous list item
      const last = currentList[currentList.length - 1]
      if (last) {
        last.text += ' ' + line.trim()
      }
      continue
    }

    flushList()
    currentParagraph.push(line.trim())
  }

  if (inCodeBlock && codeLines.length > 0) {
    blocks.push({ type: 'codeblock', lines: codeLines })
  }
  flushParagraph()
  flushList()

  return blocks
}

export function ReleaseNotesMarkdown({
  content,
  defaultExpanded = false,
}: ReleaseNotesMarkdownProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)

  if (!content || !content.trim()) {
    return null
  }

  const rawLines = content.trim().split('\n')
  const isLong = rawLines.length > 9 || content.length > 450

  const blocks = parseBlocks(content)

  return (
    <div className="space-y-1">
      <span className="text-xs font-medium text-muted-foreground">Release Notes:</span>
      <div
        className="rounded-lg border border-border/40 bg-muted/20 p-3 text-xs"
        data-testid="release-notes-container"
      >
        <div
          className={
            isLong && !isExpanded
              ? 'relative max-h-56 overflow-hidden transition-[max-height] duration-200'
              : 'relative transition-[max-height] duration-200'
          }
          data-testid="release-notes-content"
        >
          <div className="space-y-2">
            {blocks.map((block, blockIdx) => {
              const key = `block-${blockIdx}`
              switch (block.type) {
                case 'h1':
                  return (
                    <h3 key={key} className="text-sm font-semibold text-foreground pt-1 pb-0.5">
                      {renderInline(parseInlineTokens(block.content || ''), key)}
                    </h3>
                  )
                case 'h2':
                  return (
                    <h4
                      key={key}
                      className="text-[11px] font-bold uppercase tracking-wider text-muted-foreground pt-2 pb-0.5"
                    >
                      {renderInline(parseInlineTokens(block.content || ''), key)}
                    </h4>
                  )
                case 'h3':
                  return (
                    <h5 key={key} className="text-xs font-semibold text-foreground pt-1.5 pb-0.5">
                      {renderInline(parseInlineTokens(block.content || ''), key)}
                    </h5>
                  )
                case 'paragraph':
                  return (
                    <p key={key} className="text-xs leading-relaxed text-foreground/90">
                      {renderInline(parseInlineTokens(block.content || ''), key)}
                    </p>
                  )
                case 'codeblock':
                  return (
                    <pre
                      key={key}
                      className="my-1.5 overflow-x-auto rounded-md border border-border/40 bg-muted/50 p-2.5 font-mono text-xs text-foreground/90"
                    >
                      <code>{(block.lines || []).join('\n')}</code>
                    </pre>
                  )
                case 'list':
                  return (
                    <ul key={key} className="my-1 space-y-1 pl-4 text-xs text-foreground/90">
                      {(block.listItems || []).map((item, itemIdx) => {
                        const itemKey = `${key}-item-${itemIdx}`
                        const isSubItem = item.indent > 0
                        return (
                          <li
                            key={itemKey}
                            className={
                              isSubItem
                                ? 'list-circle ml-3 text-muted-foreground'
                                : 'list-disc marker:text-muted-foreground/60'
                            }
                          >
                            {renderInline(parseInlineTokens(item.text), itemKey)}
                          </li>
                        )
                      })}
                    </ul>
                  )
                default:
                  return null
              }
            })}
          </div>

          {isLong && !isExpanded && (
            <div
              className="pointer-events-none absolute inset-x-0 bottom-0 h-14 bg-gradient-to-t from-background/95 via-background/60 to-transparent"
              data-testid="fade-overlay"
            />
          )}
        </div>

        {isLong && (
          <div className="pt-2 border-t border-border/20 mt-2 flex justify-start">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs font-medium text-primary hover:text-primary/90 hover:bg-muted/40 gap-1.5 -ml-1"
              onClick={() => setIsExpanded(!isExpanded)}
              data-testid="toggle-expand-btn"
            >
              {isExpanded ? (
                <>
                  <ChevronUp className="h-3.5 w-3.5" />
                  <span>Weniger anzeigen</span>
                </>
              ) : (
                <>
                  <ChevronDown className="h-3.5 w-3.5" />
                  <span>Mehr anzeigen</span>
                </>
              )}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
