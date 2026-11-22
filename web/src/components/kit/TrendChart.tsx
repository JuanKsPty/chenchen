/**
 * <TrendChart points label tone? resting? pointLabel? zeroLine? className? />
 *
 *   points  [{day: 'YYYY-MM-DD', cents, partial}], oldest first. Fewer than two draw
 *           nothing: a single figure is not a trend.
 *   label   the chart's name for screen readers: "Patrimonio neto, mes a mes".
 *   tone    'default'  a veranera line (chart-3) on a surface card.
 *           'inverse'  a white line on the veranera block (NetWorthCard).
 *   resting     the point read when nothing points at the chart; the last by default.
 *   pointLabel  (point, index, last) => the caption's words for a point; by default
 *               "Hoy" for the last and the month for the rest ("Octubre 2026").
 *   zeroLine    a dotted line at zero, when zero is within the line's range.
 *
 * A line with a soft area under it, one point per month's end and the last one today.
 * A stretch that touches a partial point (a month some account could not be read) is
 * dashed: the figure is real, but it is not all of it.
 *
 * Above the line, the point being read: the resting one unless a pointer is over the chart, or
 * the chart has focus and the arrow keys move along it (Home and End jump to the
 * ends). The caption is a live region, so moving with the keys reads each month.
 *
 * The SVG stretches to the box (preserveAspectRatio none) with non-scaling strokes, so
 * it fills any width without distorting the line; the marker is HTML on top of it,
 * placed in percentages, so it stays round.
 */
import { useId, useMemo, useState, type KeyboardEvent, type PointerEvent } from 'react'

import { Amount } from '@/components/kit/Amount'
import { monthTitle } from '@/lib/format'
import { cn } from '@/lib/utils'

export interface TrendPoint {
  day: string
  cents: number
  partial: boolean
}

const WIDTH = 100
const HEIGHT = 40

export function TrendChart({
  points,
  label,
  tone = 'default',
  resting,
  pointLabel = monthOrToday,
  zeroLine = false,
  className,
}: {
  points: TrendPoint[]
  label: string
  tone?: 'default' | 'inverse'
  resting?: number
  pointLabel?: (point: TrendPoint, index: number, last: number) => string
  zeroLine?: boolean
  className?: string
}) {
  const captionId = useId()
  const [active, setActive] = useState<number | null>(null)

  const geometry = useMemo(() => plot(points), [points])
  const last = points.length - 1
  const index = active ?? resting ?? last
  const point = points[index]
  const at = geometry?.coords[index]
  if (!geometry || !point || !at) return null

  const pick = (event: PointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect()
    if (box.width === 0) return
    const ratio = Math.min(1, Math.max(0, (event.clientX - box.left) / box.width))
    setActive(Math.round(ratio * last))
  }

  const step = (event: KeyboardEvent<HTMLDivElement>) => {
    const moves: Record<string, number> = {
      ArrowLeft: index - 1,
      ArrowRight: index + 1,
      Home: 0,
      End: last,
    }
    const next = moves[event.key]
    if (next === undefined) return
    event.preventDefault()
    setActive(Math.min(last, Math.max(0, next)))
  }

  const inverse = tone === 'inverse'

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <p
        id={captionId}
        aria-live="polite"
        className={cn(
          'flex items-baseline justify-between gap-3 text-small',
          inverse ? 'text-primary-foreground/80' : 'text-muted-foreground',
        )}
      >
        <span className="truncate">
          {pointLabel(point, index, last)}
          {point.partial && ' · incompleto'}
        </span>
        <Amount
          amount={point.cents}
          size="inline"
          className={inverse ? 'text-primary-foreground' : 'text-foreground'}
        />
      </p>
      <div
        role="group"
        aria-label={label}
        aria-describedby={captionId}
        tabIndex={0}
        onPointerMove={pick}
        onPointerDown={pick}
        onPointerLeave={() => setActive(null)}
        onBlur={() => setActive(null)}
        onKeyDown={step}
        className={cn(
          'relative h-16 touch-pan-y rounded-md outline-none focus-visible:ring-2 lg:h-20',
          inverse
            ? 'text-primary-foreground focus-visible:ring-primary-foreground/60'
            : 'text-chart-3 focus-visible:ring-ring',
        )}
      >
        <svg
          aria-hidden="true"
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          preserveAspectRatio="none"
          className="absolute inset-0 size-full overflow-visible"
        >
          <path d={geometry.area} fill="currentColor" opacity={inverse ? 0.16 : 0.1} />
          {zeroLine && geometry.zero !== null && (
            <path
              d={`M0 ${geometry.zero}H${WIDTH}`}
              stroke="currentColor"
              strokeWidth={1}
              strokeDasharray="2 3"
              opacity={0.5}
              vectorEffect="non-scaling-stroke"
            />
          )}
          <path
            d={geometry.solid}
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
          />
          <path
            d={geometry.dashed}
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeDasharray="4 4"
            strokeLinecap="round"
            opacity={0.6}
            vectorEffect="non-scaling-stroke"
          />
        </svg>
        <span
          aria-hidden="true"
          className={cn(
            'pointer-events-none absolute size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full ring-2',
            inverse ? 'bg-primary-foreground ring-primary' : 'bg-chart-3 ring-card',
          )}
          style={{ left: `${(at.x / WIDTH) * 100}%`, top: `${(at.y / HEIGHT) * 100}%` }}
        />
      </div>
    </div>
  )
}

/**
 * Where each point goes in the viewBox, and the paths through them: the area under
 * the whole line, the solid stretches and the dashed ones. Null under two points.
 */
function plot(points: TrendPoint[]) {
  if (points.length < 2) return null
  const values = points.map((p) => p.cents)
  const min = Math.min(...values)
  const max = Math.max(...values)
  // A flat line sits in the middle; otherwise 10% of room above and below.
  const pad = max === min ? Math.max(1, Math.abs(max)) : (max - min) * 0.1
  const low = min - pad
  const high = max + pad
  const coords = points.map((p, i) => ({
    x: (i / (points.length - 1)) * WIDTH,
    y: HEIGHT - ((p.cents - low) / (high - low)) * HEIGHT,
  }))

  let solid = ''
  let dashed = ''
  for (let i = 1; i < coords.length; i++) {
    const a = coords[i - 1]
    const b = coords[i]
    if (!a || !b) continue
    const piece = `M${a.x} ${a.y}L${b.x} ${b.y}`
    if (points[i - 1]?.partial || points[i]?.partial) dashed += piece
    else solid += piece
  }
  const line = coords.map((c, i) => `${i === 0 ? 'M' : 'L'}${c.x} ${c.y}`).join('')
  const area = `${line}L${WIDTH} ${HEIGHT}L0 ${HEIGHT}Z`
  const zero = low < 0 && high > 0 ? HEIGHT - (-low / (high - low)) * HEIGHT : null
  return { coords, solid, dashed, area, zero }
}

/** "Hoy" for the last point, the month for the rest. */
function monthOrToday(point: TrendPoint, index: number, last: number): string {
  return index === last ? 'Hoy' : monthTitle(point.day)
}
