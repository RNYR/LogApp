import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Person } from '../../lib/api'
import { Alert, Button, Card, Field, Input, PageHeader } from '../../components/ui'
export function PersonnelView() {
 const {t}=useTranslation();const client=useQueryClient();const query=useQuery({queryKey:['personnel'],queryFn:()=>api.personnel.list()})
 const [name,setName]=useState('');const [editing,setEditing]=useState<Person|null>(null);const [error,setError]=useState('');const [busy,setBusy]=useState(false)
 async function save(person?:Person,active?:boolean){setBusy(true);setError('');try{
 if(person) await api.personnel.update(person.id,active===undefined?{name,version:person.version}:{active,version:person.version})
 else await api.personnel.create({name})
 setName('');setEditing(null);await client.invalidateQueries({queryKey:['personnel']});await client.invalidateQueries({queryKey:['items']})
 }catch(e){setError(e instanceof Error?e.message:t('errors.network'))}finally{setBusy(false)}}
 return <div className="space-y-4"><PageHeader title={t('personnel.title')} description={t('rental.directoryHelp')}/>
 {error&&<Alert variant="error">{error}</Alert>}
 <Card className="space-y-3 p-5"><Field label={t('rental.personName')}>{({id})=><Input id={id} value={name} disabled={busy} onChange={e=>setName(e.target.value)} maxLength={255}/>}</Field><div className="flex gap-2"><Button disabled={busy||!name.trim()} onClick={()=>void save(editing??undefined)}>{editing?t('common.save'):t('rental.addPerson')}</Button>{editing&&<Button variant="secondary" onClick={()=>{setEditing(null);setName('')}}>{t('common.cancel')}</Button>}</div></Card>
 <Card className="p-5">{query.isPending?<p>{t('common.loading')}</p>:query.isError?<Alert variant="error">{t('rental.loadError')}<Button onClick={()=>void query.refetch()}>{t('common.retry')}</Button></Alert>:<ul className="divide-y divide-stroke">{query.data?.map(person=><li key={person.id} className="flex flex-wrap items-center justify-between gap-3 py-3"><span className="text-content">{person.name} <span className="text-xs text-content-muted">{!person.active?t('rental.inactive'):person.is_customer?t('rental.customerPermanent'):''}</span></span>{!person.is_customer&&<div className="flex gap-2"><Button size="sm" variant="secondary" disabled={busy} onClick={()=>{setEditing(person);setName(person.name)}}>{t('common.edit')}</Button><Button size="sm" variant="ghost" disabled={busy} onClick={()=>void save(person,!person.active)}>{person.active?t('users.deactivate'):t('users.activate')}</Button></div>}</li>)}</ul>}</Card>
 </div>
}
