/**
 * The chart layer, over Recharts.
 *
 * One chart type so far, BarCompare, because everything plotted here is read
 * one bar at a time. A line chart was written first and deleted when the bars
 * replaced it: a round is a discrete event, and a line between two rounds draws
 * a path through scores that were never shot. A line will be right for
 * something genuinely continuous - a handicap index between rounds, say - and
 * is forty lines when that arrives.
 *
 * Everything here exists so that adding a chart is a few lines rather than a
 * fresh set of decisions about axes, tooltips, colours, and empty states. A
 * screen should say what it wants plotted; it should not have to remember that
 * the grid is slate-200 in light mode and slate-800 in dark, or that a series
 * of one point draws nothing.
 *
 * Colours come from CSS variables declared in index.css rather than from props,
 * so a chart follows the theme without re-rendering and no chart can forget to.
 */
import type { ReactNode } from 'react'
import type { AxisDomainItem, DataKey } from 'recharts'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { cx } from './ui'

/** The palette, in the order series should be assigned. */
export const SERIES_COLORS = [
  'var(--chart-series-1)',
  'var(--chart-series-2)',
  'var(--chart-series-3)',
  'var(--chart-series-4)',
]

const GRID = 'var(--chart-grid)'
const AXIS = 'var(--chart-axis)'
const LABEL = 'var(--chart-label)'

/** Shared axis and tooltip styling, so every chart looks like the same app. */
const axisProps = {
  stroke: AXIS,
  tick: { fill: LABEL, fontSize: 11 },
  tickLine: false,
  axisLine: false,
} as const

const tooltipProps = {
  contentStyle: {
    background: 'var(--chart-surface)',
    border: `1px solid ${GRID}`,
    borderRadius: '0.5rem',
    fontSize: '0.8rem',
    // Recharts sets a default shadow that reads as a grey smear on a dark
    // surface; the border above is doing the separating.
    boxShadow: 'none',
  },
  labelStyle: { color: LABEL, marginBottom: '0.25rem' },
  cursor: { stroke: AXIS, strokeDasharray: '3 3' },
} as const

/**
 * A titled box around a chart, with the empty state handled once.
 *
 * `empty` is checked here rather than in each chart because "nothing to plot"
 * is a property of the data, and a chart drawing empty axes over no data looks
 * like a bug rather than an absence.
 */
export function ChartCard({
  title,
  subtitle,
  empty,
  emptyMessage = 'Nothing to chart yet.',
  action,
  children,
  className,
}: {
  title: string
  subtitle?: string
  empty?: boolean
  emptyMessage?: string
  action?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cx('card space-y-3 p-5', className)}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 className="font-semibold">{title}</h2>
          {subtitle && (
            <p className="text-xs text-slate-500 dark:text-slate-400">{subtitle}</p>
          )}
        </div>
        {action}
      </div>
      {empty ? (
        <p className="py-10 text-center text-sm text-slate-500 dark:text-slate-400">
          {emptyMessage}
        </p>
      ) : (
        children
      )}
    </div>
  )
}

/**
 * One plotted measure.
 *
 * `key` uses Recharts' own DataKey rather than `keyof T & string`, which is
 * what the library's props actually accept: it resolves to the keys of T whose
 * values are plottable, so a typo or a non-numeric field is a compile error at
 * the call site rather than an empty line at runtime.
 */
export interface Series<T> {
  key: DataKey<T>
  label: string
  /** Defaults to the palette in order. */
  color?: string
  /** Renders the value in the tooltip and on the axis. */
  format?: (value: number) => string
}

interface BarProps<T> {
  data: T[]
  xKey: DataKey<T>
  series: Series<T>[]
  height?: number
  /** Lays the bars along the left instead, which fits long labels like club names. */
  horizontal?: boolean
  /** Colours each bar individually rather than colouring by series. */
  colorByPoint?: boolean
  /**
   * Stacks the series into one bar per category.
   *
   * Only correct when the series partition a whole - holes by result against
   * par, strokes split into putts and everything else. Stacking measures that
   * merely share an axis produces a total nobody asked for: two percentages of
   * different denominators stacked to 140% is not a fact about anything.
   */
  stacked?: boolean
  /** Draws a dashed rule, e.g. at level par. */
  reference?: { value: number; label?: string }
  /** Colours each bar from its own value, for a measure that has a good side. */
  barColor?: (value: number) => string
  /**
   * The value axis. Includes zero by default, and deliberately so.
   *
   * A bar encodes its value as a length, so an axis that starts partway up
   * lies about the ratio between bars: putts running 29 to 36 on an axis
   * starting at 28 makes the worst round look eight times the best. A line
   * chart survives truncation because it encodes position; a bar does not.
   *
   * The default is `min(0, dataMin)` rather than a flat 0, so that a measure
   * which does go negative - a score under par, or a differential from
   * shooting below the course rating - draws downward instead of being clipped
   * by its own floor. Override only to cap the top, as a percentage does.
   */
  yDomain?: [AxisDomainItem, AxisDomainItem]
}

/** Includes zero without clipping anything that falls below it. */
const zeroBased: [AxisDomainItem, AxisDomainItem] = [
  (dataMin: number) => Math.min(0, dataMin),
  'auto',
]

/**
 * A bar chart, for measures read one bar at a time.
 *
 * The default for anything per-round: a round is a discrete event, and a line
 * between two rounds implies a path through values that were never played. Also
 * the shape for comparing categories - carry by club, scoring by par - where
 * `horizontal` fits labels that do not sit under a bar.
 */
export function BarCompare<T extends Record<string, unknown>>({
  data,
  xKey,
  series,
  height = 240,
  horizontal,
  colorByPoint,
  stacked,
  reference,
  barColor,
  yDomain = zeroBased,
}: BarProps<T>) {
  const formatValue = series[0]?.format

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart
        data={data}
        layout={horizontal ? 'vertical' : 'horizontal'}
        margin={{ top: 8, right: 8, bottom: 0, left: horizontal ? 8 : -16 }}
      >
        <CartesianGrid stroke={GRID} strokeDasharray="3 3" horizontal={!horizontal} vertical={horizontal} />
        {horizontal ? (
          <>
            <XAxis type="number" {...axisProps} tickFormatter={formatValue} domain={yDomain} />
            <YAxis type="category" dataKey={xKey} {...axisProps} width={92} />
          </>
        ) : (
          <>
            <XAxis dataKey={xKey} {...axisProps} minTickGap={16} />
            <YAxis {...axisProps} width={48} tickFormatter={formatValue} domain={yDomain} />
          </>
        )}
        <Tooltip
          {...tooltipProps}
          cursor={{ fill: GRID, fillOpacity: 0.4 }}
          formatter={(value, name) => {
            const s = series.find((item) => item.label === name)
            return [s?.format ? s.format(Number(value)) : String(value), name]
          }}
        />
        {series.length > 1 && <Legend wrapperStyle={{ fontSize: '0.75rem', color: LABEL }} />}
        {reference && (
          <ReferenceLine
            {...(horizontal ? { x: reference.value } : { y: reference.value })}
            stroke={AXIS}
            strokeDasharray="4 4"
            label={
              reference.label
                ? { value: reference.label, position: 'insideTopRight', fill: LABEL, fontSize: 11 }
                : undefined
            }
          />
        )}
        {series.map((s, i) => {
          // Only the last segment of a stack gets rounded corners, or every
          // layer would show a gap where its rounding meets the one above.
          const last = i === series.length - 1
          const radius: [number, number, number, number] =
            stacked && !last ? [0, 0, 0, 0] : horizontal ? [0, 4, 4, 0] : [4, 4, 0, 0]
          return (
            <Bar
              key={s.label}
              dataKey={s.key}
              name={s.label}
              stackId={stacked ? 'stack' : undefined}
              fill={s.color ?? SERIES_COLORS[i % SERIES_COLORS.length]}
              radius={radius}
            >
              {colorByPoint &&
                data.map((_, index) => (
                  <Cell key={index} fill={SERIES_COLORS[index % SERIES_COLORS.length]} />
                ))}
              {barColor &&
                data.map((datum, index) => (
                  <Cell key={index} fill={barColor(Number(datum[s.key as keyof T]))} />
                ))}
            </Bar>
          )
        })}
      </BarChart>
    </ResponsiveContainer>
  )
}
