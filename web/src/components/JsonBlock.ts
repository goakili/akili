// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Pretty-printed JSON (or plain text) in a code block with light syntax colouring and a copy button.
// Built from VNodes: every token is a text node, so untrusted content cannot execute.
import { defineComponent, h, ref, type VNode } from 'vue'
import { prettyJSON } from '../lib/format'
import { copyText } from '../lib/clipboard'
import Icon from './Icon'

const TOKEN_RE = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g

function highlight(text: string): (string | VNode)[] {
  const out: (string | VNode)[] = []
  let last = 0
  let m: RegExpExecArray | null
  const re = new RegExp(TOKEN_RE.source, 'g')
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index))
    if (m[1] !== undefined) {
      out.push(h('span', { class: m[2] ? 'j-key' : 'j-str' }, m[1]))
      if (m[2]) out.push(m[2])
    } else if (m[3] !== undefined) out.push(h('span', { class: 'j-lit' }, m[3]))
    else if (m[4] !== undefined) out.push(h('span', { class: 'j-num' }, m[4]))
    last = m.index + m[0].length
    if (m[0].length === 0) re.lastIndex++
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

export default defineComponent({
  name: 'JsonBlock',
  props: {
    value: { type: null, required: false, default: undefined },
    error: { type: Boolean, default: false },
    /** Treat the value as plain text (tool output) rather than JSON. */
    plain: { type: Boolean, default: false },
    copy: { type: Boolean, default: true },
    maxHeight: { type: String, default: undefined },
  },
  setup(props) {
    const copied = ref(false)
    return () => {
      const text = props.plain ? String(props.value ?? '') || '(no output)' : prettyJSON(props.value) || '(empty)'
      const isJSON = !props.plain && text.length < 60000 && /^[[{]/.test(text.trim())
      const pre = h(
        'pre',
        { class: ['code', { error: props.error }], style: props.maxHeight ? { maxHeight: props.maxHeight } : undefined },
        isJSON ? highlight(text) : text,
      )
      if (!props.copy) return pre
      return h('div', { class: 'code-wrap' }, [
        pre,
        h(
          'button',
          {
            type: 'button',
            class: 'btn btn-xs copy-btn',
            'aria-label': copied.value ? 'Copied' : 'Copy to clipboard',
            onClick: async () => {
              copied.value = await copyText(text)
              setTimeout(() => (copied.value = false), 1500)
            },
          },
          [h(Icon, { name: copied.value ? 'check' : 'copy' }), copied.value ? 'Copied' : 'Copy'],
        ),
      ])
    }
  },
})
