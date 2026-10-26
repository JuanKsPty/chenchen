/**
 * The chenchén brand marks. The only file outside ui/ allowed to hold hex values,
 * because the logo colours are fixed and do not follow the theme.
 *
 *   <Wordmark size?="bar" | "landing" | "mobile" tone?="default" | "on-primary" className? />
 *     Live text in Gabarito 800, tracking -0.02em, letters in currentColor
 *     (text-foreground unless the parent says otherwise) and the tilde as a
 *     veranera bar at -45 degrees. About 24px tall in the desktop bar, 28 on the
 *     landing page and 22 in a mobile bar, tilde included. `on-primary` is for a
 *     veranera ground: white letters and a mango tilde. Exposes role="img" with
 *     aria-label "chenchén"; inside a link, give the link its own aria-label.
 *
 *   <Isotipo size?={56} title?="chenchén" className? />
 *     The c and its tilde (spec logo.svgSketch, 64 grid). The c is always
 *     veranera, the tilde follows text-foreground, so it is right in both modes.
 *     Pass title={null} where it is decorative (it then gets aria-hidden).
 *
 *   <AppIcon size?={64} title?="chenchén" className? />
 *     The app icon: flat veranera square, white c, mango tilde. No radius is
 *     drawn; round it with a wrapper if a screen needs to show it.
 *
 *   <AssistantIcon variant?="desktop" | "mobile" className? />
 *     message-circle with the brand bar, 24 grid, stroke 2, currentColor and
 *     aria-hidden. "desktop" is the 85% bubble used at 20px, "mobile" the larger
 *     bubble used at 24px. Size it with size-5 / size-6.
 *
 *   <Illustration name="empty" | "import" className? />
 *     The empty-state art, decorative (alt=""), 160px wide on mobile and 200
 *     from md (4:3). The art is an opaque PNG on the light background, so it is
 *     always rounded 16px, and in dark mode it sits on a padded light tile
 *     instead of being hidden.
 *
 *   heroImage: { src, srcSet, width, height }
 *     The landing photo, 1280 and 1920 wide. Use with sizes="…" on an <img>.
 */
import heroUrl from '@/assets/hero.jpg'
import hero1920Url from '@/assets/hero-1920.jpg'
import illEmptyUrl from '@/assets/ill-empty.png'
import illImportUrl from '@/assets/ill-import.png'
import { cn } from '@/lib/utils'

/** The only colour in the brand that is not a UI token. */
const MANGO = '#FFC247'

const WORDMARK_FONT_SIZE = {
  bar: 28,
  landing: 32,
  mobile: 26,
} as const

export function Wordmark({
  size = 'bar',
  tone = 'default',
  className,
}: {
  size?: keyof typeof WORDMARK_FONT_SIZE
  tone?: 'default' | 'on-primary'
  className?: string
}) {
  return (
    <span
      role="img"
      aria-label="chenchén"
      translate="no"
      className={cn(
        'inline-flex font-display leading-none font-extrabold tracking-[-0.02em] whitespace-nowrap select-none',
        tone === 'on-primary' && 'text-primary-foreground',
        className,
      )}
      style={{ fontSize: WORDMARK_FONT_SIZE[size] }}
    >
      <span aria-hidden="true" className="inline-flex">
        chench
        <span className="relative inline-block">
          e
          {/* The tilde, redrawn as the brand bar: 0.55 of the x-height long,
              about the n's stem thick, over the e's right shoulder and 0.18 of
              the x-height above it. */}
          <span
            className={cn(
              'absolute top-[0.078em] left-[0.1875em] h-[0.125em] w-[0.29em] -rotate-45 rounded-full',
              tone === 'default' && 'bg-primary',
            )}
            style={tone === 'on-primary' ? { backgroundColor: MANGO } : undefined}
          />
        </span>
        n
      </span>
    </span>
  )
}

export function Isotipo({
  size = 56,
  title = 'chenchén',
  className,
}: {
  size?: number
  title?: string | null
  className?: string
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      width={size}
      height={size}
      fill="none"
      strokeLinecap="round"
      className={cn('shrink-0', className)}
      {...(title ? { role: 'img', 'aria-label': title } : { 'aria-hidden': true })}
    >
      <path d="M39.7 29A14 14 0 1 0 39.7 47" strokeWidth={10} className="stroke-primary" />
      <path d="M36 15L44 7" strokeWidth={8} className="stroke-foreground" />
    </svg>
  )
}

export function AppIcon({
  size = 64,
  title = 'chenchén',
  className,
}: {
  size?: number
  title?: string | null
  className?: string
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      width={size}
      height={size}
      fill="none"
      strokeLinecap="round"
      className={cn('shrink-0', className)}
      {...(title ? { role: 'img', 'aria-label': title } : { 'aria-hidden': true })}
    >
      <rect width="64" height="64" className="fill-primary" />
      <path d="M38.4 29.9A11 11 0 1 0 38.4 44.1" stroke="#FFFFFF" strokeWidth={8} />
      <path d="M35.5 18.5L41.5 12.5" stroke={MANGO} strokeWidth={6.5} />
    </svg>
  )
}

export function AssistantIcon({
  variant = 'desktop',
  className,
}: {
  variant?: 'desktop' | 'mobile'
  className?: string
}) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={cn('size-5 shrink-0', className)}
    >
      {variant === 'desktop' ? (
        <path
          d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z"
          transform="translate(0.3 3.3) scale(0.85)"
          strokeWidth={2.35}
        />
      ) : (
        <path d="M7.02 20.3A7.65 7.65 0 1 0 3.7 16.99L2 22Z" />
      )}
      <path d="M18 6L21 3" />
    </svg>
  )
}

const ILLUSTRATION_SRC = {
  empty: illEmptyUrl,
  import: illImportUrl,
} as const

export function Illustration({
  name,
  className,
}: {
  name: keyof typeof ILLUSTRATION_SRC
  className?: string
}) {
  return (
    <img
      src={ILLUSTRATION_SRC[name]}
      alt=""
      width={400}
      height={298}
      decoding="async"
      className={cn(
        // The PNG is opaque on the light background (#FCF8F8). Rounded, it reads
        // as a soft tile on a white card; on a dark page it gets a padded light
        // tile on purpose instead of being hidden.
        'h-auto w-40 shrink-0 rounded-2xl select-none md:w-50',
        'dark:bg-[#FCF8F8] dark:p-2',
        className,
      )}
    />
  )
}

export const heroImage = {
  src: heroUrl,
  srcSet: `${heroUrl} 1280w, ${hero1920Url} 1920w`,
  width: 1280,
  height: 850,
} as const
