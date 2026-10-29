import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { Item, ItemInput, ItemPatch, StatusOption, TypeOption } from '../../lib/api'
import { Button, Field, Input, Select, TableCell } from '../../components/ui'

/**
 * Mutable, form-friendly representation of an inventory item.
 *
 * Dates use `''` for "not set" (the API accepts `''` to clear a date). `id` and
 * `version` are `null` for unsaved new entries.
 */
export interface EditableItem {
  id: number | null
  assetTag: string
  name: string
  sn: string
  type: string
  status: string
  sentDate: string
  returnDate: string
  note: string
  version: number | null
}

export function emptyEditableItem(): EditableItem {
  return {
    id: null,
    assetTag: '',
    name: '',
    sn: '',
    type: '',
    status: 'Feasibility',
    sentDate: '',
    returnDate: '',
    note: '',
    version: null,
  }
}

export function toEditableItem(item: Item): EditableItem {
  return {
    id: item.id,
    assetTag: item.asset_tag,
    name: item.name,
    sn: item.sn,
    type: item.type ?? '',
    status: item.status ?? '',
    sentDate: item.sent_date ?? '',
    returnDate: item.return_date ?? '',
    note: item.note,
    version: item.version,
  }
}

/** Body for `POST /items` (omits the tag when blank so the server auto-assigns). */
export function editableToInput(draft: EditableItem): ItemInput {
  return {
    ...(draft.assetTag.trim() ? { asset_tag: draft.assetTag.trim() } : {}),
    name: draft.name.trim(),
    sn: draft.sn.trim(),
    ...(draft.type ? { type: draft.type } : {}),
    ...(draft.status ? { status: draft.status } : {}),

  }
}

/** Body for `PATCH /items/{id}` (optimistic concurrency via `version`). */
export function editableToPatch(draft: EditableItem): ItemPatch {
  if (draft.version === null) {
    throw new Error('Cannot build an item patch without a version')
  }
  return {
    version: draft.version,
    name: draft.name.trim(),
    sn: draft.sn.trim(),
    type: draft.type,

  }
}

export interface ItemEditorProps {
  draft: EditableItem
  types: TypeOption[]
  statuses: StatusOption[]
  saving?: boolean
  onChange: (changes: Partial<EditableItem>) => void
  onSave?: () => void
  onCancel?: () => void
  submitLabel?: string
  /** Replace the default Save/Cancel cell (used for the new-entry buffer). */
  actions?: ReactNode
}

/**
 * Inline table-row editor used both for the "new entries" buffer and for
 * editing an existing inventory row. Controls use visually hidden labels to
 * retain table density while keeping every field associated.
 */
export function ItemEditor({
  draft,
  types,
  statuses,
  saving = false,
  onChange,
  onSave,
  onCancel,
  submitLabel,
  actions,
}: ItemEditorProps) {
  const { t } = useTranslation()
  const selectedStatus = statuses.find((status) => status.value === draft.status) ?? null
  const canSave = draft.name.trim().length > 0 && !saving

  return (
    <tr className="bg-action-selected">
      <TableCell>
        {actions ?? (
          <div className="flex items-center gap-1">
            <Button size="sm" variant="primary" onClick={onSave} disabled={!canSave} loading={saving}>
              {submitLabel ?? t('common.save')}
            </Button>
            <Button size="sm" variant="ghost" onClick={onCancel} disabled={saving}>
              {t('common.cancel')}
            </Button>
          </div>
        )}
      </TableCell>

      <TableCell>
        <span className="font-mono text-xs text-content-muted">{draft.assetTag || '—'}</span>
      </TableCell>

      <TableCell>
        <Field label={t('inventory.columns.name')} srOnlyLabel>
          {({ id, 'aria-describedby': describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              value={draft.name}
              placeholder={t('common.name')}
              onChange={(event) => onChange({ name: event.target.value })}
            />
          )}
        </Field>
      </TableCell>

      <TableCell>
        <Field label={t('inventory.columns.sn')} srOnlyLabel>
          {({ id, 'aria-describedby': describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              value={draft.sn}
              placeholder={t('common.sn')}
              onChange={(event) => onChange({ sn: event.target.value })}
            />
          )}
        </Field>
      </TableCell>

      <TableCell>
        <Field label={t('inventory.columns.type')} srOnlyLabel>
          {({ id, 'aria-describedby': describedBy }) => (
            <Select
              id={id}
              aria-describedby={describedBy}
              value={draft.type}
              onChange={(event) => onChange({ type: event.target.value })}
            >
              <option value="">—</option>
              {types.map((type) => (
                <option key={type.value} value={type.value}>
                  {type.label}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </TableCell>

      <TableCell>
        <div className="flex items-center gap-2">
          <span
            className="inline-block h-3 w-3 shrink-0 rounded-full border border-stroke"
            style={{ backgroundColor: selectedStatus?.color ?? 'transparent' }}
            aria-hidden="true"
          />
          <Field label={t('inventory.columns.status')} srOnlyLabel className="flex-1">
            {({ id, 'aria-describedby': describedBy }) => (
              <Select
                id={id}
                aria-describedby={describedBy}
                disabled
                value={draft.status}
                onChange={(event) => onChange({ status: event.target.value })}
              >
                <option value="">—</option>
                {statuses.map((status) => (
                  <option key={status.value} value={status.value}>
                    {status.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
      </TableCell>

      <TableCell>
        <Field label={t('inventory.columns.sent')} srOnlyLabel>
          {({ id, 'aria-describedby': describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              type="date"
              disabled
              value={draft.sentDate}
              onChange={(event) => onChange({ sentDate: event.target.value })}
            />
          )}
        </Field>
      </TableCell>

      <TableCell>
        <Field label={t('inventory.columns.return')} srOnlyLabel>
          {({ id, 'aria-describedby': describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              type="date"
              disabled
              value={draft.returnDate}
              onChange={(event) => onChange({ returnDate: event.target.value })}
            />
          )}
        </Field>
      </TableCell>

      <TableCell>
        <Field label={t('inventory.columns.note')} srOnlyLabel>
          {({ id, 'aria-describedby': describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              disabled
              value={draft.note}
              placeholder={t('common.note')}
              onChange={(event) => onChange({ note: event.target.value })}
            />
          )}
        </Field>
      </TableCell>
    </tr>
  )
}
