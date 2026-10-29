import { useTranslation } from 'react-i18next'
import { Field, Input, Select } from '../../components/ui'
import type { BulkPatch, Item, Person, StatusOption } from '../../lib/api'
import { isFeasibility } from './rules'
export function RentalFields({ patch, onChange, items, people, statuses, admin, busy = false }: {
 patch: BulkPatch; onChange: (patch: BulkPatch) => void; items: Item[]; people: Person[]; statuses: StatusOption[]; admin: boolean; busy?: boolean
}) {
 const { t } = useTranslation()
 const locked = items.length === 0 || items.some(i => isFeasibility(patch.status ?? i.status))
 const selectedPerson = people.find(p => p.id === patch.personnel_id)
 function changeStatus(status: string) {
  // Locked fields never enter a Feasibility request; historical values survive.
  onChange(isFeasibility(status) ? { status } : { ...patch, status: status || undefined })
 }
 return <div className="space-y-3">
  <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3 [&>*]:min-w-0">
   <Field label={t('inventory.columns.status')}>{({id})=><Select id={id} value={!admin && isFeasibility(patch.status) ? '' : patch.status ?? ''} disabled={busy} onChange={e=>changeStatus(e.target.value)}>
    <option value="">{t('rental.keepCurrent')}</option>
    {statuses.filter(s=>admin || !isFeasibility(s.value)).map(s=><option key={s.value} value={s.value}>{s.label}</option>)}
   </Select>}</Field>
   <Field label={t('rental.personnel')}>{({id})=><Select id={id} disabled={busy || locked} value={patch.personnel_id ?? ''} onChange={e=>onChange({...patch,personnel_id:e.target.value===''?undefined:Number(e.target.value)})}>
    <option value="">{t('rental.keepCurrent')}</option><option value="0">{t('rental.selectPerson')}</option>
    {people.map(p=><option key={p.id} value={p.id} disabled={!p.active}>{p.name}{!p.active ? ` (${t('rental.inactive')})` : ''}</option>)}
   </Select>}</Field>
   <Field label={t('inventory.columns.sent')}>{({id})=><Input id={id} type="date" disabled={busy || locked} value={patch.sent_date ?? ''} onChange={e=>onChange({...patch,sent_date:e.target.value})}/>}</Field>
   <Field label={t('inventory.columns.return')}>{({id})=><Input id={id} type="date" disabled={busy || locked} value={patch.return_date ?? ''} onChange={e=>onChange({...patch,return_date:e.target.value})}/>}</Field>
   <Field label={t('inventory.columns.note')} className="md:col-span-2">{({id})=><Input id={id} disabled={busy || locked} value={patch.note ?? ''} placeholder={selectedPerson?.is_customer ? t('rental.customerHelp') : t('rental.keepCurrent')} onChange={e=>onChange({...patch,note:e.target.value})}/>}</Field>
  </div>
  <p className="text-xs text-content-muted">{locked ? t('rental.locked') : t('rental.requiredHelp')}</p>
 </div>
}
