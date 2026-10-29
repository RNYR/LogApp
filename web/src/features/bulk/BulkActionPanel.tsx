import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type BulkPatch, type Item, type StatusOption } from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { Button, Card, ConfirmDialog, Field, Input, Notice as NoticeMessage, Select } from '../../components/ui'

export interface BulkActionPanelProps {
  /** Matched, buffered items to act on (each carries its current `version`). */
  items: Item[]
  statuses: StatusOption[]
  /** Called with the fresh items returned by a successful apply/undo. */
  onUpdated: (items: Item[]) => void
}

/** The editable bulk-action form. */
export interface BulkForm {
  status: string
  sentDate: string
  returnDate: string
  note: string
  clearDates: boolean
  clearNote: boolean
}

/** Pre-apply state of one item, used to build the undo patch. */
export interface PreviousState {
  status: string
  sentDate: string
  returnDate: string
  note: string
}

export interface UndoGroup {
  items: Item[]
  patch: BulkPatch
}

interface UndoSnapshot {
  /** Pre-apply state, keyed by `asset_tag`. */
  previous: Map<string, PreviousState>
  /** Items as returned by the apply call (new versions). */
  updated: Item[]
}

/**
 * Build the uniform patch for the form, or `null` when nothing was selected.
 * `''` clears a date/reference (API semantics); omitted fields are unchanged.
 */
export function buildBulkPatch(form: BulkForm): BulkPatch | null {
  const patch: BulkPatch = {}
  let changed = false

  if (form.status) {
    patch.status = form.status
    changed = true
  }

  if (form.clearDates) {
    patch.clear_sent_date = true
    patch.clear_return_date = true
    changed = true
  } else {
    if (form.sentDate) {
      patch.sent_date = form.sentDate
      changed = true
    }
    if (form.returnDate) {
      patch.return_date = form.returnDate
      changed = true
    }
  }

  if (form.clearNote) {
    patch.note = ''
    changed = true
  } else if (form.note.trim()) {
    patch.note = form.note.trim()
    changed = true
  }

  return changed ? patch : null
}

/**
 * Group items that need the same undo patch so each group can be restored with
 * a single transactional `POST /items/bulk` call.
 */
export function buildUndoGroups(updated: Item[], previous: Map<string, PreviousState>): UndoGroup[] {
  const groups = new Map<string, { patch: BulkPatch; items: Item[] }>()

  for (const item of updated) {
    const prev = previous.get(item.asset_tag)
    if (!prev) continue

    const patch: BulkPatch = { status: prev.status, note: prev.note }
    if (prev.sentDate) patch.sent_date = prev.sentDate
    else patch.clear_sent_date = true
    if (prev.returnDate) patch.return_date = prev.returnDate
    else patch.clear_return_date = true

    const signature = JSON.stringify(patch)
    const group = groups.get(signature)
    if (group) group.items.push(item)
    else groups.set(signature, { patch, items: [item] })
  }

  return [...groups.values()]
}

/**
 * Transactional bulk editor for the scan buffer.
 *
 * Applies one uniform patch (status / dates / note) to every buffered item via
 * `POST /items/bulk` (all-or-nothing, per-item version check). Undo restores the
 * exact pre-apply state by grouping items with identical previous values and
 * re-applying one bulk patch per group against the new versions.
 */
export function BulkActionPanel({ items, statuses, onUpdated }: BulkActionPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [status, setStatus] = useState('')
  const [sentDate, setSentDate] = useState('')
  const [returnDate, setReturnDate] = useState('')
  const [note, setNote] = useState('')
  const [clearDates, setClearDates] = useState(false)
  const [clearNote, setClearNote] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; message: string } | null>(null)
  const [undo, setUndo] = useState<UndoSnapshot | null>(null)
  const undoRef = useRef<HTMLButtonElement | null>(null)

  const form: BulkForm = { status, sentDate, returnDate, note, clearDates, clearNote }
  const patch = buildBulkPatch(form)

  function resetForm(): void {
    setStatus('')
    setSentDate('')
    setReturnDate('')
    setNote('')
    setClearDates(false)
    setClearNote(false)
  }

  const apply = useMutation({
    mutationFn: async () => {
      if (!patch) throw new Error('No changes to apply')

      const previous = new Map<string, PreviousState>()
      for (const item of items) {
        previous.set(item.asset_tag, {
          status: item.status ?? '',
          sentDate: item.sent_date ?? '',
          returnDate: item.return_date ?? '',
          note: item.note,
        })
      }

      const result = await api.items.bulk({
        items: items.map((item) => ({ asset_tag: item.asset_tag, version: item.version })),
        patch,
      })
      return { result, previous }
    },
    onSuccess: ({ result, previous }) => {
      setUndo({ previous, updated: result.updated_items })
      setNotice({ kind: 'success', message: t('bulk.appliedCount', { count: result.updated }) })
      onUpdated(result.updated_items)
      void queryClient.invalidateQueries({ queryKey: ['items'] })
      resetForm()
      setConfirming(false)
    },
    onError: (error) => {
      setNotice({ kind: 'error', message: apiErrorMessage(error, t) })
      if (error instanceof ApiError && error.isVersionConflict) {
        void queryClient.invalidateQueries({ queryKey: ['items'] })
      }
      setConfirming(false)
    },
  })

  const revert = useMutation({
    mutationFn: async (snapshot: UndoSnapshot) => {
      const updatedItems: Item[] = []
      for (const group of buildUndoGroups(snapshot.updated, snapshot.previous)) {
        const result = await api.items.bulk({
          items: group.items.map((item) => ({ asset_tag: item.asset_tag, version: item.version })),
          patch: group.patch,
        })
        updatedItems.push(...result.updated_items)
      }
      return updatedItems
    },
    onSuccess: (updatedItems) => {
      setUndo(null)
      setNotice({ kind: 'success', message: t('bulk.undoApplied') })
      onUpdated(updatedItems)
      void queryClient.invalidateQueries({ queryKey: ['items'] })
    },
    onError: (error) => setNotice({ kind: 'error', message: apiErrorMessage(error, t) }),
  })

  if (items.length === 0) {
    return (
      <Card className="p-4">
        <h2 className="text-base font-semibold text-content">{t('bulk.title')}</h2>
        <p className="mt-2 text-sm text-content-muted">{t('bulk.noItems')}</p>
      </Card>
    )
  }

  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-content">{t('bulk.title')}</h2>
          <p className="text-xs text-content-muted">
            {t('scan.bufferCount', { count: items.length })}
            {` · ${t('bulk.apply', { count: items.length })}`}
          </p>
        </div>
        {undo ? (
          <Button ref={undoRef} variant="secondary" size="sm" loading={revert.isPending} onClick={() => revert.mutate(undo)}>
            {t('bulk.undo')}
          </Button>
        ) : null}
      </div>

      {notice ? (
        <NoticeMessage
          variant={notice.kind}
          onDismiss={() => setNotice(null)}
          dismissLabel={t('common.close')}
        >
          {notice.message}
        </NoticeMessage>
      ) : null}

      <div className="grid gap-3 md:grid-cols-2">
        <Field label={t('bulk.applyStatus')}>
          {({ id, 'aria-describedby': describedBy }) => (
            <Select
              id={id}
              aria-describedby={describedBy}
              value={status}
              onChange={(event) => setStatus(event.target.value)}
            >
              <option value="">{t('bulk.selectStatus')}</option>
              {statuses.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </Select>
          )}
        </Field>

        <Field label={t('bulk.note')}>
          {({ id, 'aria-describedby': describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              value={note}
              disabled={clearNote}
              placeholder={t('bulk.note')}
              onChange={(event) => setNote(event.target.value)}
            />
          )}
        </Field>

        <fieldset className="grid gap-2 md:col-span-2">
          <legend className="text-xs font-medium text-content-muted">{t('bulk.setDates')}</legend>
          <div className="flex flex-wrap items-end gap-3">
            <Field label={t('bulk.sentDate')}>
              {({ id, 'aria-describedby': describedBy }) => (
                <Input
                  id={id}
                  aria-describedby={describedBy}
                  type="date"
                  value={sentDate}
                  disabled={clearDates}
                  onChange={(event) => setSentDate(event.target.value)}
                />
              )}
            </Field>
            <Field label={t('bulk.returnDate')}>
              {({ id, 'aria-describedby': describedBy }) => (
                <Input
                  id={id}
                  aria-describedby={describedBy}
                  type="date"
                  value={returnDate}
                  disabled={clearDates}
                  onChange={(event) => setReturnDate(event.target.value)}
                />
              )}
            </Field>
            <label className="flex items-center gap-2 text-sm text-content">
              <input
                type="checkbox"
                className="accent-action"
                checked={clearDates}
                onChange={(event) => setClearDates(event.target.checked)}
              />
              {t('bulk.clearDates')}
            </label>
          </div>
        </fieldset>

        <label className="flex items-center gap-2 text-sm text-content">
          <input
            type="checkbox"
            className="accent-action"
            checked={clearNote}
            onChange={(event) => setClearNote(event.target.checked)}
          />
          {t('bulk.clearNote')}
        </label>
      </div>

      <div className="flex items-center justify-end gap-2">
        <Button
          variant="secondary"
          size="sm"
          disabled={!patch}
          onClick={() => {
            resetForm()
            setNotice(null)
          }}
        >
          {t('common.clear')}
        </Button>
        <Button variant="primary" size="sm" disabled={!patch || revert.isPending} onClick={() => setConfirming(true)}>
          {t('bulk.apply', { count: items.length })}
        </Button>
      </div>

      <ConfirmDialog
        open={confirming}
        busy={apply.isPending}
        title={t('bulk.title')}
        confirmLabel={t('common.confirm')}
        cancelLabel={t('common.cancel')}
        onConfirm={() => apply.mutate()}
        onCancel={() => setConfirming(false)}
        fallbackFocusRef={undoRef}
      >
        {t('bulk.confirmApply', { count: items.length })}
      </ConfirmDialog>
    </Card>
  )
}
