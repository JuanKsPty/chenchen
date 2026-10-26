/**
 * The layout ladder. Every horizontal gutter and width cap comes from here, so a
 * page cannot drift out of step with the chrome around it.
 *
 * Spec (shape.densityNotes, pageLayout): content at most 1120px wide, a 16px
 * gutter on mobile and 32px from md.
 *
 *   shell    the page container: centred, capped at 74rem with the gutter inside
 *            it, so the content itself is the spec's 1120px from md (1184 − 2 × 32)
 *   gutter   the gutter alone, for chrome that spans the viewport (bars, landing)
 *   measure  the cap for a reading surface: prose, a form, an explanation
 *
 * The chrome breakpoint is lg (1024px), not md: the desktop bar and the assistant
 * panel from lg, the mobile top bar, tab bar and assistant sheet below. Anything a
 * page shows only beside the desktop bar (its visible h1, an overflow button by the
 * title) switches at lg too; below lg the page talks to the bar with useTopBar().
 * md stays free for density (padding, row heights, two columns).
 */

export const shell = 'mx-auto w-full max-w-[74rem] px-4 md:px-8'

export const gutter = 'px-4 md:px-8'

export const measure = 'max-w-[34rem]'
