# Sklad roles and functions

Current requirements implemented 2026-10-16. These replace the previous Viewer / authenticated Editor model. The approved visual design remains PASS.

| Function | Public Editor (no login) | Authenticated Admin |
|---|---|---|
| Inventory search, filters, sorting and pagination | Yes | Yes |
| Scan labels / select inventory checkboxes | Yes | Yes |
| Shared rental buffer: bulk, individual, remove, clear | Yes | Yes |
| Change status away from Feasibility | Yes | Yes |
| Confirm physical return by setting Feasibility | No | Yes |
| Edit sent date, return date, renter and note outside Feasibility | Yes | Yes |
| Create equipment, edit identity/type, archive, view archives, export | No | Yes |
| Manual PT-E300 label preparation | No | Yes |
| Personnel add, rename, activate/deactivate | No | Yes |
| Options, settings, integrations and account password | No | Yes |
| About in sidebar | Yes | Yes |

Public navigation contains Inventory, Scan and About. Admin additionally sees Labels, Manage options, Settings, Personnel and Accounts. Viewer is removed. Editor is an anonymous mode, not a login account. Only the configured Admin account may authenticate; account provisioning is disabled.

## Rental rules

Inventory checkboxes and scans add existing active equipment to one buffer, retained across Inventory/Scan navigation. Duplicates are ignored. Users can apply one patch to all selected items or edit and save individual items. Clearing/removing from the buffer never deletes equipment. The buffer is session UI state, not durable storage.

Feasibility is the default for new equipment and means Admin has verified physical return. Its machine name cannot be renamed or deleted. Selecting it locks sent date, return date, personnel and note. Selecting another status unlocks those fields before saving. Admin return confirmation preserves previous rental details and does not require them.

Every saved update resulting in another status requires a personnel reference. The permanent Customer entry also requires a nonblank Note; the interface explicitly requests the customer's name. The system validates nonblank text, not the identity or accuracy of a name.

Deactivated people remain linked to existing rentals and may remain on updates to that ongoing rental. They cannot be assigned to another item or a new rental leaving Feasibility. Admin can reactivate them. Customer cannot be renamed or deactivated.

The API validates these rules independently of the UI. Bulk requests are transactional: a validation/version failure rolls back every item. Validation identifies the affected asset; the interface lists missing information for all selected items before submission. Public requests accept only rental fields and require same-origin JSON. Administrative sessions retain CSRF protection.

## Migration

Migration 0002 adds personnel and item references, seeds Customer and ensures Feasibility exists. It preserves item statuses, notes and history; legacy items are not automatically marked returned or assigned invented renters. A legacy item outside Feasibility needs a renter on its next update. Old non-Admin accounts are deactivated and their sessions revoked; records remain for audit references.

Public rental status menus omit Feasibility entirely, including individual editing. Existing Feasibility remains visible as the item’s current status and keeps rental fields locked until another status is selected. Feasibility, Devel and Sales use those exact names in all languages; other status labels remain localizable. Migration 0003 updates stored translations.
