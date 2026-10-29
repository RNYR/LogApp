import { useRentals } from '../rentals/RentalContext'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  api,
  ApiError,
  type Item,
  type ItemListParams,
  type SortField,
  type SortOrder,
} from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { useSession } from '../../lib/auth/sessionContext'
import { useLocale } from '../../lib/i18n/useLocale'
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  Field,
  IconButton,
  Input,
  LoadingState,
  Notice as NoticeMessage,
  PageHeader,
  Select,
  Table,
  TableCell,
  TableHead,
  TableHeader,
  TableScroll,
} from '../../components/ui'
import { cn } from '../../lib/utils/cn'
import {
  editableToInput,
  editableToPatch,
  emptyEditableItem,
  ItemEditor,
  toEditableItem,
  type EditableItem,
} from './ItemEditor'

const PAGE_SIZES = [10, 20, 50, 100]
const NUM_COLUMNS = 9

const SORTABLE_COLUMNS: ReadonlyArray<{ field: SortField; labelKey: string }> = [
  { field: 'asset_tag', labelKey: 'inventory.columns.id' },
  { field: 'name', labelKey: 'inventory.columns.name' },
  { field: 'sn', labelKey: 'inventory.columns.sn' },
  { field: 'type', labelKey: 'inventory.columns.type' },
  { field: 'status', labelKey: 'inventory.columns.status' },
  { field: 'sent_date', labelKey: 'inventory.columns.sent' },
  { field: 'return_date', labelKey: 'inventory.columns.return' },
  { field: 'note', labelKey: 'inventory.columns.note' },
]

interface BufferRow {
  key: string
  draft: EditableItem
}

interface Notice {
  kind: 'success' | 'error'
  message: string
}

function newKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return Math.random().toString(36).slice(2)
}

function formatDate(value: string | null, locale: string): string {
  if (!value) return '—'
  const parsed = new Date(`${value}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return value
  return new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'short', day: 'numeric' }).format(parsed)
}

function pageNumbers(current: number, total: number): Array<number | '…'> {
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1)
  const wanted = new Set<number>([1, total, current, current - 1, current + 1])
  const sorted = [...wanted].filter((page) => page >= 1 && page <= total).sort((a, b) => a - b)
  const result: Array<number | '…'> = []
  let previous = 0
  for (const page of sorted) {
    if (previous && page - previous > 1) result.push('…')
    result.push(page)
    previous = page
  }
  return result
}

function SortHeader({
  field,
  label,
  activeField,
  order,
  onToggle,
}: {
  field: SortField
  label: string
  activeField: SortField
  order: SortOrder
  onToggle: (field: SortField) => void
}) {
  const active = field === activeField
  const ariaSort: 'ascending' | 'descending' | 'none' = active
    ? order === 'desc'
      ? 'descending'
      : 'ascending'
    : 'none'
  return (
    <TableHeader scope="col" aria-sort={ariaSort}>
      <button type="button" className="inline-flex items-center gap-1 hover:text-content" onClick={() => onToggle(field)}>
        {label}
        <span aria-hidden="true" className={cn('text-[10px]', active ? 'text-action' : 'text-transparent')}>
          {active && order === 'desc' ? '▼' : '▲'}
        </span>
      </button>
    </TableHeader>
  )
}

/**
 * Inventory page: searchable/filterable/sortable table with pagination, an
 * inline row editor, and the "new entries" buffer.
 */
export function InventoryTable() {
  const { t } = useTranslation()
  const { locale } = useLocale()
  const { user } = useSession()
  const queryClient = useQueryClient()
  const canEdit = user?.role === 'admin'
  const rentals = useRentals()
  const perPageId = useId()

  // --- query / filter state -------------------------------------------------
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const [onlyOverdue, setOnlyOverdue] = useState(false)
  const [sort, setSort] = useState<SortField>('asset_tag')
  const [order, setOrder] = useState<SortOrder>('asc')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)

  // --- editor state ---------------------------------------------------------
  const [editDraft, setEditDraft] = useState<EditableItem | null>(null)
  const [buffer, setBuffer] = useState<BufferRow[]>([])
  const [archiveTarget, setArchiveTarget] = useState<Item | null>(null)
  const [notice, setNotice] = useState<Notice | null>(null)

  // Focus fallback for the archive dialog when the archived row is removed.
  const refreshButtonRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(searchInput.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [searchInput])

  const params = useMemo<ItemListParams>(
    () => ({
      q: search || undefined,
      type: typeFilter || undefined,
      status: statusFilter || undefined,
      archived: (canEdit && showArchived) || undefined,
      overdue: onlyOverdue || undefined,
      sort,
      order,
      page,
      per_page: perPage,
    }),
    [canEdit, search, typeFilter, statusFilter, showArchived, onlyOverdue, sort, order, page, perPage],
  )

  const optionsQuery = useQuery({
    queryKey: ['options', locale],
    queryFn: ({ signal }) => api.options.get(locale, signal),
  })

  const itemsQuery = useQuery({
    queryKey: ['items', params],
    queryFn: ({ signal }) => api.items.list(params, signal),
    placeholderData: keepPreviousData,
  })

  const types = optionsQuery.data?.types ?? []
  const statuses = optionsQuery.data?.statuses ?? []
  const typeLabels = useMemo(() => new Map(types.map((entry) => [entry.value, entry.label])), [types])
  const statusMap = useMemo(() => new Map(statuses.map((entry) => [entry.value, entry])), [statuses])

  const invalidateItems = () => void queryClient.invalidateQueries({ queryKey: ['items'] })

  const saveEdit = useMutation({
    mutationFn: (draft: EditableItem) => {
      if (draft.id === null) throw new Error('Missing item id')
      return api.items.patch(draft.id, editableToPatch(draft))
    },
    onSuccess: () => {
      setEditDraft(null)
      setNotice({ kind: 'success', message: t('inventory.itemSaved') })
      invalidateItems()
    },
    onError: (error) => {
      setNotice({ kind: 'error', message: apiErrorMessage(error, t) })
      if (error instanceof ApiError && error.isVersionConflict) invalidateItems()
    },
  })

  const saveBuffer = useMutation({
    mutationFn: async (drafts: EditableItem[]) => {
      let created = 0
      for (const draft of drafts) {
        await api.items.create(editableToInput(draft))
        created += 1
      }
      return created
    },
    onSuccess: () => {
      setBuffer([])
      setNotice({ kind: 'success', message: t('inventory.itemCreated') })
      invalidateItems()
    },
    onError: (error) => {
      setNotice({ kind: 'error', message: apiErrorMessage(error, t) })
      invalidateItems()
    },
  })

  const archiveItem = useMutation({
    mutationFn: (item: Item) => api.items.archive(item.id),
    onSuccess: () => {
      setArchiveTarget(null)
      setNotice({ kind: 'success', message: t('inventory.itemArchived') })
      invalidateItems()
    },
    onError: (error) => {
      setArchiveTarget(null)
      setNotice({ kind: 'error', message: apiErrorMessage(error, t) })
    },
  })

  const items = itemsQuery.data?.data ?? []
  const meta = itemsQuery.data?.meta
  const total = meta?.total ?? 0
  const totalPages = Math.max(1, meta?.total_pages ?? 1)
  const start = total === 0 ? 0 : (page - 1) * perPage + 1
  const end = Math.min(page * perPage, total)
  const hasFilters = Boolean(search || typeFilter || statusFilter || showArchived || onlyOverdue)

  const [exporting, setExporting] = useState(false)
  async function downloadInventory(format: 'csv' | 'json') {
    setExporting(true)
    try {
      const blob = await api.export.download(format, showArchived)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `sklad-${showArchived ? 'archived' : 'active'}.${format}`
      link.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (error) {
      setNotice({ kind: 'error', message: apiErrorMessage(error, t) })
    } finally { setExporting(false) }
  }

  function toggleSort(field: SortField) {
    if (sort === field) setOrder(order === 'asc' ? 'desc' : 'asc')
    else {
      setSort(field)
      setOrder('asc')
    }
    setPage(1)
  }

  function clearFilters() {
    setSearchInput('')
    setSearch('')
    setTypeFilter('')
    setStatusFilter('')
    setShowArchived(false)
    setOnlyOverdue(false)
    setPage(1)
  }

  function addBufferEntry() {
    setBuffer((rows) => [...rows, { key: newKey(), draft: emptyEditableItem() }])
  }

  function updateBufferEntry(key: string, changes: Partial<EditableItem>) {
    setBuffer((rows) => rows.map((row) => (row.key === key ? { ...row, draft: { ...row.draft, ...changes } } : row)))
  }

  function removeBufferEntry(key: string) {
    setBuffer((rows) => rows.filter((row) => row.key !== key))
  }

  const bufferValid = buffer.length > 0 && buffer.every((row) => row.draft.name.trim().length > 0)

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('inventory.title')} description={t(canEdit ? 'shell.inventoryHint' : 'rental.inventoryHelp')}
        actions={
          <>
            <Button ref={refreshButtonRef} variant="secondary" size="sm" onClick={() => void itemsQuery.refetch()}>
              {t('common.refresh')}
            </Button>
            {canEdit ? (
              <Button variant="primary" size="sm" onClick={addBufferEntry}>
                + {t('inventory.addEntry')}
              </Button>
            ) : null}
          </>
        }
      />

      {notice ? (
        <NoticeMessage
          variant={notice.kind}
          onDismiss={() => setNotice(null)}
          dismissLabel={t('common.close')}
        >
          {notice.message}
        </NoticeMessage>
      ) : null}

      {/* Filters ----------------------------------------------------------- */}
      <Card className="space-y-3 p-4">
        <div className="flex flex-wrap items-end gap-3">
          <Field label={t('common.search')} className="flex-1 basis-64">
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={describedBy}
                value={searchInput}
                placeholder={t('inventory.searchPlaceholder')}
                onChange={(event) => setSearchInput(event.target.value)}
              />
            )}
          </Field>

          <Field label={t('common.type')} className="min-w-[8.5rem] flex-1 sm:flex-none">
            {({ id, 'aria-describedby': describedBy }) => (
              <Select
                id={id}
                aria-describedby={describedBy}
                value={typeFilter}
                onChange={(event) => { setTypeFilter(event.target.value); setPage(1) }}
              >
                <option value="">{t('common.all')}</option>
                {types.map((type) => (
                  <option key={type.value} value={type.value}>
                    {type.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>

          <Field label={t('common.status')} className="min-w-[8.5rem] flex-1 sm:flex-none">
            {({ id, 'aria-describedby': describedBy }) => (
              <Select
                id={id}
                aria-describedby={describedBy}
                value={statusFilter}
                onChange={(event) => { setStatusFilter(event.target.value); setPage(1) }}
              >
                <option value="">{t('common.all')}</option>
                {statuses.map((status) => (
                  <option key={status.value} value={status.value}>
                    {status.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>

          <div className="flex basis-full flex-wrap items-center gap-x-5 gap-y-2 sm:basis-auto sm:pb-2.5">
          {canEdit ? (
          <label className="flex items-center gap-2 text-sm text-content">
            <input
              type="checkbox"
              className="accent-action"
              checked={showArchived}
              onChange={(event) => { setShowArchived(event.target.checked); setPage(1) }}
            />
            {t('inventory.archived')}
          </label>
          ) : null}

          <label className="flex items-center gap-2 text-sm text-content">
            <input
              type="checkbox"
              className="accent-action"
              checked={onlyOverdue}
              onChange={(event) => { setOnlyOverdue(event.target.checked); setPage(1) }}
            />
            {t('inventory.overdue')}
          </label>
          </div>

          {hasFilters ? (
            <Button variant="ghost" size="sm" onClick={clearFilters}>
              {t('common.clear')}
            </Button>
          ) : null}
        </div>
      </Card>

      {/* New-entry buffer -------------------------------------------------- */}
      {canEdit && buffer.length > 0 ? (
        <Card className="space-y-3 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold text-content">{t('inventory.newEntriesBuffer')}</h2>
              <p className="text-xs text-content-muted">{t('inventory.bufferWillAdd')}</p>
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="primary"
                size="sm"
                disabled={!bufferValid}
                loading={saveBuffer.isPending}
                onClick={() => saveBuffer.mutate(buffer.map((row) => row.draft))}
              >
                {t('common.save')}
              </Button>
              <Button variant="secondary" size="sm" onClick={() => setBuffer([])} disabled={saveBuffer.isPending}>
                {t('common.cancel')}
              </Button>
            </div>
          </div>

          <TableScroll>
            <Table className="inventory-table min-w-[1000px]">
              <TableHead>
                <tr>
                  <TableHeader />
                  {SORTABLE_COLUMNS.map((column) => (
                    <TableHeader key={column.field} scope="col">
                      {t(column.labelKey)}
                    </TableHeader>
                  ))}
                </tr>
              </TableHead>
              <tbody>
                {buffer.map((row) => (
                  <ItemEditor
                    key={row.key}
                    draft={row.draft}
                    types={types}
                    statuses={statuses}
                    onChange={(changes) => updateBufferEntry(row.key, changes)}
                    actions={
                      <IconButton
                        label={t('common.remove')}
                        className="text-content-muted hover:bg-danger-bg hover:text-danger-fg"
                        onClick={() => removeBufferEntry(row.key)}
                      >
                        ×
                      </IconButton>
                    }
                  />
                ))}
              </tbody>
            </Table>
          </TableScroll>
        </Card>
      ) : null}

      {/* Inventory table --------------------------------------------------- */}
      <Card className="p-4">
        <TableScroll>
          <Table className="inventory-table min-w-[1000px]">
            <TableHead>
              <tr>
                <TableHeader scope="col" className="w-px whitespace-nowrap"><input type="checkbox" className="h-4 w-4" aria-label={t('rental.selectPage')} disabled={!items.some(i=>!i.archived)} checked={items.length>0 && items.filter(i=>!i.archived).every(i=>rentals.items.some(r=>r.id===i.id))} onChange={e=>e.target.checked?rentals.add(items):items.forEach(i=>rentals.remove(i.id))} /></TableHeader>
                {SORTABLE_COLUMNS.map((column) => (
                  <SortHeader
                    key={column.field}
                    field={column.field}
                    label={t(column.labelKey)}
                    activeField={sort}
                    order={order}
                    onToggle={toggleSort}
                  />
                ))}
              </tr>
            </TableHead>
            <tbody>
              {itemsQuery.isPending ? (
                <tr>
                  <td colSpan={NUM_COLUMNS} className="px-3 py-12">
                    <LoadingState label={t('common.loading')} />
                  </td>
                </tr>
              ) : itemsQuery.isError ? (
                <tr>
                  <td colSpan={NUM_COLUMNS} className="px-3 py-6">
                    <Alert variant="error" title={t('common.error')}>
                      <div className="flex items-center justify-between gap-4">
                        <span>{apiErrorMessage(itemsQuery.error, t)}</span>
                        <Button size="sm" variant="secondary" onClick={() => void itemsQuery.refetch()}>
                          {t('common.retry')}
                        </Button>
                      </div>
                    </Alert>
                  </td>
                </tr>
              ) : items.length === 0 ? (
                <tr>
                  <td colSpan={NUM_COLUMNS}>
                    <EmptyState title={hasFilters ? t('inventory.emptyFiltered') : t('inventory.emptyState')} />
                  </td>
                </tr>
              ) : (
                items.map((item) =>
                  editDraft && editDraft.id === item.id ? (
                    <ItemEditor
                      key={item.id}
                      draft={editDraft}
                      types={types}
                      statuses={statuses}
                      saving={saveEdit.isPending}
                      onChange={(changes) => setEditDraft((prev) => (prev ? { ...prev, ...changes } : prev))}
                      onSave={() => saveEdit.mutate(editDraft)}
                      onCancel={() => setEditDraft(null)}
                    />
                  ) : (
                    <ItemRow
                      key={item.id}
                      item={item}
                      canEdit={canEdit}
                      selected={rentals.items.some(i=>i.id===item.id)}
                      onSelect={()=>rentals.items.some(i=>i.id===item.id)?rentals.remove(item.id):rentals.add([item])}
                      typeLabel={typeLabels.get(item.type ?? '') ?? item.type_label ?? item.type ?? '—'}
                      statusLabel={
                        statusMap.get(item.status ?? '')?.label ?? item.status_label ?? item.status ?? '—'
                      }
                      statusColor={statusMap.get(item.status ?? '')?.color ?? item.status_color}
                      sentDate={formatDate(item.sent_date, locale)}
                      returnDate={formatDate(item.return_date, locale)}
                      onEdit={() => setEditDraft(toEditableItem(item))}
                      onArchive={() => setArchiveTarget(item)}
                    />
                  ),
                )
              )}
            </tbody>
          </Table>
        </TableScroll>

        {/* Pagination ------------------------------------------------------ */}
        <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
          {canEdit ? <div className="flex flex-col gap-2 text-xs text-content-muted sm:flex-row sm:items-center"><span>{t('inventory.exportAll')}</span><div className="flex gap-2"><Button variant="secondary" size="sm" disabled={exporting} onClick={() => void downloadInventory('csv')}>CSV</Button><Button variant="secondary" size="sm" disabled={exporting} onClick={() => void downloadInventory('json')}>JSON</Button></div></div> : null}
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm text-content-muted">
            <label htmlFor={perPageId}>{t('pagination.showEntries')}</label>
            <Select
              id={perPageId}
              className="!h-9 !w-24 shrink-0"
              value={perPage}
              onChange={(event) => { setPerPage(Number(event.target.value)); setPage(1) }}
            >
              {PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </Select>
            <span className="min-w-0 break-words">
              {total === 0
                ? t('common.noEntries')
                : `${t('pagination.showing')} ${start}–${end} ${t('pagination.of')} ${total}`}
              {hasFilters ? ` ${t('pagination.filtered')}` : ''}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-1">
            <Button
              size="sm"
              variant="secondary"
              disabled={page <= 1}
              onClick={() => setPage((current) => Math.max(1, current - 1))}
            >
              {t('pagination.previous')}
            </Button>
            {pageNumbers(page, totalPages).map((entry, index) =>
              entry === '…' ? (
                <span key={`gap-${index}`} className="px-2 text-content-muted">
                  …
                </span>
              ) : (
                <Button
                  key={entry}
                  size="sm"
                  variant={entry === page ? 'primary' : 'secondary'}
                  onClick={() => setPage(entry)}
                >
                  {entry}
                </Button>
              ),
            )}
            <Button
              size="sm"
              variant="secondary"
              disabled={page >= totalPages}
              onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
            >
              {t('pagination.next')}
            </Button>
          </div>
        </div>
      </Card>

      <ConfirmDialog
        open={archiveTarget !== null}
        busy={archiveItem.isPending}
        title={t('inventory.archive')}
        confirmLabel={t('inventory.archive')}
        cancelLabel={t('common.cancel')}
        destructive
        onConfirm={() => archiveTarget && archiveItem.mutate(archiveTarget)}
        onCancel={() => setArchiveTarget(null)}
        fallbackFocusRef={refreshButtonRef}
      >
        {t('inventory.deleteConfirm')}
      </ConfirmDialog>
    </div>
  )
}

function ItemRow({
  item,
  canEdit,
  selected, onSelect,
  typeLabel,
  statusLabel,
  statusColor,
  sentDate,
  returnDate,
  onEdit,
  onArchive,
}: {
  item: Item
  canEdit: boolean
  selected: boolean
  onSelect: () => void
  typeLabel: string
  statusLabel: string
  statusColor: string | null | undefined
  sentDate: string
  returnDate: string
  onEdit: () => void
  onArchive: () => void
}) {
  const { t } = useTranslation()
  return (
    <tr className="hover:bg-panel-subtle/60">
      <TableCell className="w-px whitespace-nowrap">
        <div className={canEdit ? "-my-3 flex flex-nowrap items-center gap-0.5" : "flex h-5 flex-nowrap items-center"}>
        <input type="checkbox" className="mr-1 h-4 w-4 shrink-0" aria-label={`${t("rental.select")} ${item.asset_tag}`} disabled={item.archived} checked={selected} onChange={onSelect} />
        {canEdit ? (
          <>
            <IconButton label={t('common.edit')} className="text-content-muted" onClick={onEdit}>
              ✎
            </IconButton>
            <IconButton
              label={t('inventory.archive')}
              className="text-content-muted hover:bg-danger-bg hover:text-danger-fg"
              onClick={onArchive}
            >
              ×
            </IconButton>
          </>
        ) : null}
        </div>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs">{item.asset_tag}</span>
      </TableCell>
      <TableCell>{item.name || '—'}</TableCell>
      <TableCell>{item.sn || '—'}</TableCell>
      <TableCell>{typeLabel}</TableCell>
      <TableCell>
        <Badge color={statusColor}>{statusLabel}</Badge>
      </TableCell>
      <TableCell>{sentDate}</TableCell>
      <TableCell>{returnDate}</TableCell>
      <TableCell><div>{item.personnel_name && <span className="block text-xs font-medium">{item.personnel_name}</span>}{item.note || '—'}</div></TableCell>
    </tr>
  )
}
