import { createContext, useContext, useState, type ReactNode } from 'react'
import type { Item } from '../../lib/api'
interface RentalSelection { items: Item[]; add: (items: Item[]) => void; remove: (id: number) => void; clear: () => void; update: (items: Item[]) => void }
const Context = createContext<RentalSelection | null>(null)
export function RentalProvider({ children }: { children: ReactNode }) {
 const [items, setItems] = useState<Item[]>([])
 const value: RentalSelection = {
  items,
  add: incoming => setItems(current => [...current, ...incoming.filter(i => !i.archived && !current.some(c => c.id === i.id))].filter((i,n,a)=>a.findIndex(x=>x.id===i.id)===n)),
  remove: id => setItems(current => current.filter(i => i.id !== id)),
  clear: () => setItems([]),
  update: incoming => setItems(current => current.map(i => incoming.find(x => x.id === i.id) ?? i)),
 }
 return <Context.Provider value={value}>{children}</Context.Provider>
}
export function useRentals() { const value = useContext(Context); if (!value) throw new Error('RentalProvider missing'); return value }
