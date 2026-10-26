/**
 * renderReply(text) turns an assistant reply into paragraphs and lists.
 *
 *   - A blank line ends a paragraph; each other line is its own paragraph, so a
 *     sentence followed by a list keeps its line break.
 *   - Lines starting with "•", "- " or "* " become a bulleted list, "1. " a
 *     numbered one. A leading "#" heading marker is dropped and the line is bold.
 *   - **bold** runs are 600.
 *   - Every dollar figure ("$612.35", "−$35.00", "+$3,800") is parsed and drawn
 *     through <Amount size="inline">, in 600, so the reply's figures read like the
 *     rest of the app: the real minus sign, grouped thousands, two decimals.
 */
import type { ReactNode } from 'react'

import { Amount } from '@/components/kit'

/** A dollar figure, with an optional sign that is not glued to a word ("10-$20"). */
const MONEY = /(?<![\w.$])([+\-−]?)\$\s?((?:\d{1,3}(?:,\d{3})+|\d+)(?:\.\d{1,2})?)/g
const BULLET = /^(?:•|-|\*)\s+/
const NUMBERED = /^\d{1,2}[.)]\s+/
const HEADING = /^#{1,6}\s+/

export function renderReply(text: string): ReactNode[] {
  const blocks: ReactNode[] = []
  let list: { ordered: boolean; items: string[] } | null = null

  const flush = () => {
    if (!list) return
    const key = `list-${blocks.length}`
    const items = list.items.map((item, index) => (
      <li key={index} className="pl-1">
        {inline(item)}
      </li>
    ))
    blocks.push(
      list.ordered ? (
        <ol key={key} className="flex list-decimal flex-col gap-1 pl-5">
          {items}
        </ol>
      ) : (
        <ul key={key} className="flex list-disc flex-col gap-1 pl-5">
          {items}
        </ul>
      ),
    )
    list = null
  }

  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) {
      flush()
      continue
    }

    const ordered = NUMBERED.test(line)
    if (ordered || BULLET.test(line)) {
      if (list && list.ordered !== ordered) flush()
      list ??= { ordered, items: [] }
      list.items.push(line.replace(ordered ? NUMBERED : BULLET, ''))
      continue
    }

    flush()
    if (HEADING.test(line)) {
      blocks.push(
        <p key={`h-${blocks.length}`} className="font-semibold">
          {inline(line.replace(HEADING, ''))}
        </p>,
      )
    } else {
      blocks.push(<p key={`p-${blocks.length}`}>{inline(line)}</p>)
    }
  }
  flush()

  return blocks
}

/** **bold** runs, then figures inside each run. */
function inline(text: string): ReactNode[] {
  return text.split(/(\*\*[^*]+\*\*)/g).flatMap((part, index): ReactNode[] => {
    if (part.length > 4 && part.startsWith('**') && part.endsWith('**')) {
      return [
        <strong key={`b-${index}`} className="font-semibold">
          {figures(part.slice(2, -2), `b-${index}`)}
        </strong>,
      ]
    }
    return figures(part, `t-${index}`)
  })
}

function figures(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = []
  let last = 0

  for (const match of text.matchAll(MONEY)) {
    const [whole, sign = '', digits = '0'] = match
    const start = match.index ?? 0
    if (start > last) nodes.push(text.slice(last, start))

    const magnitude = Math.round(Number(digits.replace(/,/g, '')) * 100)
    const cents = sign === '-' || sign === '−' ? -magnitude : magnitude
    nodes.push(
      <Amount
        key={`${keyPrefix}-${start}`}
        amount={cents}
        size="inline"
        signed={sign === '+'}
        className="font-semibold"
      />,
    )
    last = start + whole.length
  }

  if (last < text.length) nodes.push(text.slice(last))
  return nodes
}
