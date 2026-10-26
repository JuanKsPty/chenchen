/**
 * <ImportFilePicker file onFile disabled? error? />
 *
 *   file      the chosen File, or null
 *   onFile    called with a File the picker accepts (.txt, .csv or .xlsx); a file
 *             of any other kind is refused here with a line under the picker
 *   disabled  while an import or a check is running
 *   error     an outside message to show under the picker
 *
 * One 72px pill, a real <button> that opens the system file chooser (the <input
 * type="file"> itself stays hidden). Empty: a pink file-up tile, "Elige un archivo"
 * and the formats. Chosen: the file's name and size, and "Cambiar". From lg a file
 * can also be dropped on it.
 */
import { FileTextIcon, FileUpIcon } from 'lucide-react'
import { useEffect, useId, useRef, useState, type DragEvent } from 'react'

import { Tile } from '@/components/kit'
import { cn } from '@/lib/utils'

const ACCEPT = '.txt,.csv,.xlsx'
const ACCEPTED = /\.(txt|csv|xlsx)$/i

export function ImportFilePicker({
  file,
  onFile,
  disabled = false,
  error,
}: {
  file: File | null
  onFile: (file: File) => void
  disabled?: boolean
  error?: string | null
}) {
  const input = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [refused, setRefused] = useState<string | null>(null)
  const messageId = useId()
  const message = refused ?? error ?? null

  // A file dropped beside the picker (on the dialog or the scrim) would make the
  // browser open it and leave the app; while the picker is up, such a drop does
  // nothing instead.
  useEffect(() => {
    const swallow = (event: globalThis.DragEvent) => {
      if (event.dataTransfer?.types.includes('Files')) event.preventDefault()
    }
    window.addEventListener('dragover', swallow)
    window.addEventListener('drop', swallow)
    return () => {
      window.removeEventListener('dragover', swallow)
      window.removeEventListener('drop', swallow)
    }
  }, [])

  const take = (picked: File | undefined) => {
    if (!picked) return
    if (!ACCEPTED.test(picked.name)) {
      setRefused(`${picked.name} no es un estado de cuenta: sube un .csv, .txt o .xlsx.`)
      return
    }
    setRefused(null)
    onFile(picked)
  }

  const drop = (event: DragEvent<HTMLButtonElement>) => {
    event.preventDefault()
    setDragging(false)
    if (!disabled) take(event.dataTransfer.files[0])
  }

  return (
    <div className="flex flex-col gap-2">
      <input
        ref={input}
        type="file"
        accept={ACCEPT}
        tabIndex={-1}
        aria-hidden="true"
        className="sr-only"
        disabled={disabled}
        onChange={(event) => {
          take(event.target.files?.[0])
          event.target.value = ''
        }}
      />
      <button
        type="button"
        data-import-picker=""
        disabled={disabled}
        aria-describedby={message ? messageId : undefined}
        data-dragging={dragging || undefined}
        onClick={() => input.current?.click()}
        onDragOver={(event) => {
          event.preventDefault()
          if (!disabled) setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={drop}
        className={cn(
          'grid min-h-18 w-full grid-cols-[2.75rem_minmax(0,1fr)_auto] items-center gap-3 rounded-full bg-muted py-3 pr-5 pl-3 text-left text-foreground',
          'transition-colors duration-180 ease-out hover:bg-line data-dragging:bg-primary-soft',
          'disabled:cursor-not-allowed disabled:hover:bg-muted',
        )}
      >
        {file ? (
          <Tile icon={FileTextIcon} size="md" className="bg-card text-foreground" />
        ) : (
          <Tile icon={FileUpIcon} pending size="md" />
        )}
        <span className="flex min-w-0 flex-col">
          <span className="truncate text-body font-semibold">
            {file ? file.name : 'Elige un archivo'}
          </span>
          <span className="truncate text-small font-medium text-muted-foreground">
            {file ? (
              fileSize(file.size)
            ) : (
              <>
                .csv, .txt o .xlsx
                <span className="hidden lg:inline">, o arrástralo aquí</span>
              </>
            )}
          </span>
        </span>
        {file && <span className="text-body font-semibold text-primary-text">Cambiar</span>}
      </button>
      {message && (
        <p
          id={messageId}
          role="alert"
          className="px-4 text-small font-medium text-pretty text-destructive"
        >
          {message}
        </p>
      )}
    </div>
  )
}

/** "12 KB", "1.4 MB": how big the chosen file is, as a person reads it. */
function fileSize(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
