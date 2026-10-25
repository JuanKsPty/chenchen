import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

/**
 * tailwind-merge only knows Tailwind's stock scales. Without this it reads the
 * custom type sizes (`text-body`, `text-small`…) as text colours, so
 * `cn('text-body', 'text-muted-foreground')` silently drops the size, and it
 * reads `shadow-card` as a shadow colour. The names here must match the
 * `--text-*` and `--shadow-*` tokens in styles.css.
 */
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ['display', 'title', 'heading', 'body', 'small', 'micro'],
      shadow: ['card', 'raised', 'bar'],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Hands a fetched file to the browser as a download.
 *
 * Needed because the file arrives as a blob rather than as a navigation: the session
 * is a bearer token in a header, so the download had to be fetched rather than linked
 * to, and a blob has to be turned back into something the browser will save.
 *
 * The object URL is revoked on the next tick and not immediately. Revoking it in the
 * same task as the click races the browser's own read of it in some engines, and the
 * failure mode is a download that silently produces an empty file.
 */
export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)

  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()

  setTimeout(() => URL.revokeObjectURL(url), 0)
}
