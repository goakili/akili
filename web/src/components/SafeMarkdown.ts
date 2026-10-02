// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// A deliberately tiny Markdown renderer that builds VNodes, never HTML strings. Agent and tool output
// is untrusted: every piece of text ends up as a text node, so nothing it contains can execute.
//
// Supported: fenced code blocks, headings, bullet/numbered lists, blockquotes, paragraphs, and inline
// `code`, **bold**, *italic* and http(s) links.

import { defineComponent, h, type VNode, type VNodeArrayChildren } from 'vue'

type Inline = string | VNode

// Only .source is used: each renderInline call compiles its own instance.
const INLINE_RE = /(`+)([\s\S]*?)\1|\*\*([^*\n]+)\*\*|__([^_\n]+)__|(?<![\w*])\*([^*\n]+)\*(?!\w)|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])/g

function safeHref(url: string): string | null {
  try {
    const u = new URL(url)
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.toString() : null
  } catch {
    return null
  }
}

function link(text: string, url: string): Inline {
  const href = safeHref(url)
  if (!href) return text
  return h('a', { href, target: '_blank', rel: 'noopener noreferrer nofollow' }, text)
}

export function renderInline(text: string): Inline[] {
  // A fresh regex per call: renderInline recurses for **bold** and *italic*, and a shared global
  // regex would have its lastIndex reset by the inner call, re-matching the same span forever.
  const re = new RegExp(INLINE_RE.source, 'g')
  const out: Inline[] = []
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(text))) {
    const end = m.index + m[0].length
    if (m.index > last) out.push(text.slice(last, m.index))
    if (m[1] !== undefined) out.push(h('code', { class: 'md-code' }, m[2]))
    else if (m[3] !== undefined) out.push(h('strong', renderInline(m[3])))
    else if (m[4] !== undefined) out.push(h('strong', renderInline(m[4])))
    else if (m[5] !== undefined) out.push(h('em', renderInline(m[5])))
    else if (m[6] !== undefined && m[7] !== undefined) out.push(link(m[6], m[7]))
    else if (m[8] !== undefined) out.push(link(m[8], m[8]))
    last = end
    if (m[0].length === 0) re.lastIndex++
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function inlineLines(lines: string[]): VNodeArrayChildren {
  const out: VNodeArrayChildren = []
  lines.forEach((l, i) => {
    if (i > 0) out.push(h('br'))
    out.push(...renderInline(l))
  })
  return out
}

const FENCE = /^\s*(```+|~~~+)\s*([\w+-]*)\s*$/
const BULLET = /^\s*[-*+]\s+(.*)$/
const ORDERED = /^\s*(\d+)[.)]\s+(.*)$/
const HEADING = /^(#{1,6})\s+(.*)$/
const QUOTE = /^\s*>\s?(.*)$/

export function renderMarkdown(src: string): VNode[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const blocks: VNode[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    const fence = line.match(FENCE)
    if (fence) {
      const marker = fence[1]
      const lang = fence[2]
      const body: string[] = []
      i++
      while (i < lines.length && !lines[i].trim().startsWith(marker)) {
        body.push(lines[i])
        i++
      }
      i++ // closing fence (or EOF while streaming)
      blocks.push(
        h('pre', { class: 'md-pre', 'data-lang': lang || undefined }, [h('code', body.join('\n'))]),
      )
      continue
    }
    if (!line.trim()) {
      i++
      continue
    }
    const heading = line.match(HEADING)
    if (heading) {
      const level = Math.min(heading[1].length + 2, 6)
      blocks.push(h(`h${level}`, { class: 'md-h' }, renderInline(heading[2])))
      i++
      continue
    }
    if (BULLET.test(line) || ORDERED.test(line)) {
      const ordered = !BULLET.test(line)
      const items: VNode[] = []
      while (i < lines.length) {
        const m = ordered ? lines[i].match(ORDERED) : lines[i].match(BULLET)
        if (m) {
          items.push(h('li', renderInline(ordered ? m[2] : m[1])))
          i++
        } else if (lines[i].trim() && /^\s{2,}/.test(lines[i]) && items.length) {
          // continuation line of the previous item
          const prev = items[items.length - 1]
          items[items.length - 1] = h('li', [...((prev.children as VNodeArrayChildren) ?? []), ' ', ...renderInline(lines[i].trim())])
          i++
        } else break
      }
      blocks.push(h(ordered ? 'ol' : 'ul', { class: 'md-list' }, items))
      continue
    }
    if (QUOTE.test(line)) {
      const body: string[] = []
      while (i < lines.length && QUOTE.test(lines[i])) {
        body.push(lines[i].match(QUOTE)![1])
        i++
      }
      blocks.push(h('blockquote', { class: 'md-quote' }, inlineLines(body)))
      continue
    }
    const para: string[] = []
    while (
      i < lines.length &&
      lines[i].trim() &&
      !FENCE.test(lines[i]) &&
      !HEADING.test(lines[i]) &&
      !BULLET.test(lines[i]) &&
      !ORDERED.test(lines[i]) &&
      !QUOTE.test(lines[i])
    ) {
      para.push(lines[i])
      i++
    }
    blocks.push(h('p', inlineLines(para)))
  }
  return blocks
}

export default defineComponent({
  name: 'SafeMarkdown',
  props: { text: { type: String, default: '' } },
  setup(props) {
    return () => h('div', { class: 'md' }, renderMarkdown(props.text))
  },
})
