// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

/**
 * Splits a Server-Sent Events byte stream into the data of each event. Akili sends unnamed events,
 * one JSON object each, so only `data:` lines matter; multi-line data is joined with "\n".
 */
export class SSEParser {
  private buf = ''
  private data: string[] = []

  constructor(private readonly onData: (data: string) => void) {}

  push(chunk: string): void {
    this.buf += chunk
    let nl: number
    while ((nl = this.buf.search(/\r\n|\r|\n/)) >= 0) {
      const line = this.buf.slice(0, nl)
      this.buf = this.buf.slice(nl + (this.buf.startsWith('\r\n', nl) ? 2 : 1))
      this.line(line)
    }
  }

  private line(line: string): void {
    if (line === '') {
      if (this.data.length) this.onData(this.data.join('\n'))
      this.data = []
      return
    }
    if (line.startsWith(':')) return
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'data') this.data.push(value)
  }
}
