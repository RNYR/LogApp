import type { BulkPatch, Item, Person } from '../../lib/api'
export const isFeasibility = (value: string | null | undefined) => value?.toLowerCase() === 'feasibility'
export function rentalProblem(item: Item, patch: BulkPatch, people: Person[], admin: boolean): string | null {
 const status = patch.status ?? item.status
 if (!status) return 'statusRequired'
 if (isFeasibility(status)) return !isFeasibility(item.status) && !admin ? 'adminReturn' : null
 const id = patch.personnel_id ?? item.personnel_id
 if (!id) return 'renterRequired'
 const person = people.find(p => p.id === id)
 if (!person || (!person.active && (id !== item.personnel_id || isFeasibility(item.status)))) return 'inactivePerson'
 if (person.is_customer && !(patch.note ?? item.note).trim()) return 'customerRequired'
 return null
}
