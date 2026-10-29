import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type BulkPatch, type Item } from '../../lib/api'
import { useSession } from '../../lib/auth/sessionContext'
import { useLocale } from '../../lib/i18n/useLocale'
import { Alert, Button, Card } from '../../components/ui'
import { useRentals } from './RentalContext'
import { RentalFields } from './RentalFields'
import { isFeasibility, rentalProblem } from './rules'
export function RentalBuffer() {
 const {t}=useTranslation(); const {locale}=useLocale(); const {user}=useSession(); const admin=user?.role==='admin'
 const selection=useRentals(); const client=useQueryClient()
 const [individual,setIndividual]=useState(false); const [patch,setPatch]=useState<BulkPatch>({}); const [drafts,setDrafts]=useState<Record<number,BulkPatch>>({})
 const [busy,setBusy]=useState(false); const [error,setError]=useState(''); const [success,setSuccess]=useState('')
 const options=useQuery({queryKey:['options',locale],queryFn:()=>api.options.get(locale)})
 const directory=useQuery({queryKey:['personnel'],queryFn:()=>api.personnel.list()})
 const people=directory.data??[]; const statuses=options.data?.statuses??[]
 function initial(i:Item):BulkPatch {return isFeasibility(i.status)?{status:i.status!}:{status:i.status??'',personnel_id:i.personnel_id??0,sent_date:i.sent_date??'',return_date:i.return_date??'',note:i.note}}
 async function save(items:Item[],changes:BulkPatch) {
  setError('');setSuccess('')
  const problems=items.map(i=>{const problem=rentalProblem(i,changes,people,admin);return problem?`${i.asset_tag}: ${t(`rental.${problem}`)}`:''}).filter(Boolean)
  if(problems.length){setError(problems.join('\n'));return}
  setBusy(true)
  try {
   const updated=items.length===1 ? [await api.items.patch(items[0].id,{...changes,version:items[0].version})] : (await api.items.bulk({items:items.map(i=>({asset_tag:i.asset_tag,version:i.version})),patch:changes})).updated_items
   selection.update(updated);setDrafts(current=>{const next={...current};updated.forEach(i=>delete next[i.id]);return next});setPatch({});setSuccess(t('rental.saved',{count:updated.length}));await client.invalidateQueries({queryKey:['items']})
  } catch(e) {setError(e instanceof ApiError ? (e.details?.join('\n') || e.message) : t('errors.network'))} finally {setBusy(false)}
 }
 async function reload() {setBusy(true);try{const fresh=await api.items.resolve(selection.items.map(i=>i.asset_tag));selection.update(fresh.matched);fresh.unknown.forEach(tag=>{const item=selection.items.find(i=>i.asset_tag===tag);if(item)selection.remove(item.id)});setDrafts({});setPatch({});setError('');setSuccess('')}catch(e){setError(e instanceof Error?e.message:t('errors.network'))}finally{setBusy(false)}}
 return <Card role="region" className="mb-6 space-y-4 p-5" aria-label={t('rental.buffer')}>
  <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="text-lg font-semibold text-content">{t('rental.buffer')} <span className="text-content-muted">({selection.items.length})</span></h2><p className="text-sm text-content-muted">{t('rental.bufferHelp')}</p></div>
   {selection.items.length>0 && <div className="flex flex-wrap gap-2"><Button size="sm" variant="secondary" disabled={busy} onClick={()=>void reload()}>{t('common.refresh')}</Button><Button size="sm" variant="ghost" disabled={busy} onClick={()=>{selection.clear();setDrafts({});setPatch({});setError('');setSuccess('')}}>{t('scan.clearBuffer')}</Button></div>}
  </div>
  {error && <Alert variant="error"><div className="whitespace-pre-line">{error}</div></Alert>}
  {success && <p role="status" className="text-sm text-content">{success}</p>}
  {(options.isError || directory.isError) && <Alert variant="error">{t('rental.loadError')} <Button size="sm" onClick={()=>{void options.refetch();void directory.refetch()}}>{t('common.retry')}</Button></Alert>}
  {selection.items.length>0 && <>
   <div className="flex flex-wrap gap-2"><Button size="sm" variant={!individual?'primary':'secondary'} disabled={busy} onClick={()=>setIndividual(false)}>{t('rental.applyAll')}</Button><Button size="sm" variant={individual?'primary':'secondary'} disabled={busy} onClick={()=>setIndividual(true)}>{t('rental.individual')}</Button></div>
   <ul className="divide-y divide-stroke">{selection.items.map(item=><li key={item.id} className="space-y-3 py-3">
    <div className="flex items-start justify-between gap-3"><span className="min-w-0 flex-1 break-words pt-1.5 text-sm font-medium text-content">{item.asset_tag} · {item.name} <span className="font-normal text-content-muted">— {item.status_label??item.status} · {item.personnel_name??t('common.notSet')}</span></span><Button size="sm" variant="secondary" className="shrink-0" aria-label={`${t('common.remove')} ${item.asset_tag}`} disabled={busy} onClick={()=>{selection.remove(item.id);setError('')}}>{t('common.remove')}</Button></div>
    {individual && <><RentalFields patch={drafts[item.id]??initial(item)} onChange={draft=>setDrafts(d=>({...d,[item.id]:draft}))} items={[item]} people={people} statuses={statuses} admin={admin} busy={busy}/><Button size="sm" disabled={busy || !options.data || !directory.data} onClick={()=>void save([item],drafts[item.id]??initial(item))}>{t('common.save')} {item.asset_tag}</Button></>}
   </li>)}</ul>
   {!individual && <><RentalFields patch={patch} onChange={setPatch} items={selection.items} people={people} statuses={statuses} admin={admin} busy={busy}/><p className="text-xs text-content-muted">{t('rental.bulkHelp')}</p><Button disabled={busy || !Object.keys(patch).some(k=>patch[k as keyof BulkPatch]!==undefined) || !options.data || !directory.data} onClick={()=>void save(selection.items,patch)}>{t('rental.saveAll',{count:selection.items.length})}</Button></>}
  </>}
 </Card>
}
