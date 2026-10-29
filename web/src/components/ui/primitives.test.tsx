import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { Alert } from './Alert'
import { Badge } from './Badge'
import { ConfirmDialog } from './ConfirmDialog'
import { Field, type FieldRenderProps } from './Field'
import { IconButton } from './IconButton'
import { LoadingState } from './LoadingState'
import { Notice } from './Notice'
import { Table, TableCell, TableHead, TableHeader, TableScroll } from './Table'

/**
 * Phase 3 shared primitives.
 *
 * The Vitest environment is `node`, so these tests render to static markup and
 * assert the structural/a11y contract (associations, roles, labels, native
 * attributes) that the browser tests then exercise interactively.
 */
describe('Field', () => {
  it('associates the label and composes hint/error description ids', () => {
    const html = renderToStaticMarkup(
      createElement(Field, {
        label: 'Item name',
        hint: 'A hint',
        error: 'Required',
        children: (props: FieldRenderProps) => createElement('input', props),
      }),
    )

    expect(html).toContain('<label')
    expect(html).toContain('Item name')
    const id = html.match(/<input id="([^"]+)"/)?.[1]
    expect(id).toBeTruthy()
    expect(html).toContain(`for="${id}"`)
    expect(html).toContain(`${id}-hint`)
    expect(html).toContain(`${id}-error`)
    expect(html).toContain('aria-describedby')
  })

  it('hides the label visually when srOnlyLabel is set', () => {
    const html = renderToStaticMarkup(
      createElement(Field, {
        label: 'Hidden',
        srOnlyLabel: true,
        children: (props: FieldRenderProps) => createElement('input', props),
      }),
    )
    expect(html).toContain('sr-only')
    expect(html).toContain('for=')
  })
})

describe('Notice', () => {
  it('renders an Alert status role without dismissal', () => {
    const html = renderToStaticMarkup(createElement(Notice, { variant: 'info', children: 'Saved' }))
    expect(html).toContain('role="status"')
    expect(html).toContain('Saved')
    expect(html).not.toContain('aria-label="Close"')
  })

  it('renders an Alert alert role for errors', () => {
    const html = renderToStaticMarkup(createElement(Notice, { variant: 'error', children: 'Boom' }))
    expect(html).toContain('role="alert"')
  })

  it('renders an accessible dismiss button', () => {
    const html = renderToStaticMarkup(
      createElement(Notice, {
        variant: 'success',
        onDismiss: () => {},
        dismissLabel: 'Close',
        children: 'Done',
      }),
    )
    expect(html).toContain('<button')
    expect(html).toContain('aria-label="Close"')
    expect(html).toContain('Close')
  })
})

describe('LoadingState', () => {
  it('renders visible, politely-announced status text', () => {
    const html = renderToStaticMarkup(createElement(LoadingState, { label: 'Loading…' }))
    expect(html).toContain('role="status"')
    expect(html).toContain('aria-live="polite"')
    expect(html).toContain('Loading…')
    expect(html).toContain('<svg')
  })
})

describe('IconButton', () => {
  it('requires and exposes an accessible name with a decorative glyph', () => {
    const html = renderToStaticMarkup(createElement(IconButton, { label: 'Edit', children: '✎' }))
    expect(html).toContain('aria-label="Edit"')
    expect(html).toContain('title="Edit"')
    expect(html).toContain('aria-hidden="true"')
  })

  it('enforces a >=44px hit area, zero padding and a restored outline', () => {
    const html = renderToStaticMarkup(createElement(IconButton, { label: 'Remove', children: '×' }))
    expect(html).toContain('!h-11')
    expect(html).toContain('!w-11')
    expect(html).toContain('!p-0')
    expect(html).toContain('focus-visible:outline')
    expect(html).toContain('focus-visible:outline-action')
  })
})

describe('Table', () => {
  it('wraps native table elements and forwards aria-sort', () => {
    const html = renderToStaticMarkup(
      createElement(TableScroll, {
        children: createElement(Table, {
          children: [
            createElement(TableHead, {
              key: 'head',
              children: createElement('tr', {
                children: createElement(TableHeader, { scope: 'col', 'aria-sort': 'ascending', children: 'Name' }),
              }),
            }),
            createElement('tbody', {
              key: 'body',
              children: createElement('tr', { children: createElement(TableCell, { children: 'cell' }) }),
            }),
          ],
        }),
      }),
    )
    expect(html).toContain('overflow-x-auto')
    expect(html).toContain('<table')
    expect(html).toContain('<thead')
    expect(html).toContain('aria-sort="ascending"')
    expect(html).toContain('<td')
  })
})

describe('Badge', () => {
  it('renders a neutral badge with semantic panel/content tokens', () => {
    const html = renderToStaticMarkup(createElement(Badge, { children: 'Unknown' }))
    expect(html).toContain('bg-panel-subtle')
    expect(html).toContain('text-content')
    expect(html).not.toContain('background-color')
  })

  it('picks a contrasting foreground for supplied API colours', () => {
    const dark = renderToStaticMarkup(createElement(Badge, { color: '#0b6287', children: 'Status' }))
    expect(dark).toContain('text-content-inverse')
    expect(dark).toContain('background-color:#0b6287')

    const light = renderToStaticMarkup(createElement(Badge, { color: '#ffd966', children: 'Status' }))
    expect(light).toContain('text-content-onColor')
  })

  it('falls back to the semantic content foreground for invalid colours', () => {
    const html = renderToStaticMarkup(createElement(Badge, { color: 'bogus', children: 'Status' }))
    expect(html).toContain('text-content')
    expect(html).not.toContain('text-content-inverse')
    expect(html).not.toContain('text-content-onColor')
  })
})

describe('Alert', () => {
  it('preserves role alert for errors and role status otherwise', () => {
    expect(renderToStaticMarkup(createElement(Alert, { variant: 'error', children: 'x' }))).toContain('role="alert"')
    expect(renderToStaticMarkup(createElement(Alert, { variant: 'success', children: 'x' }))).toContain('role="status"')
  })

  it('maps variants to semantic intent tokens', () => {
    const html = renderToStaticMarkup(createElement(Alert, { variant: 'warning', children: 'x' }))
    expect(html).toContain('bg-warning-bg')
    expect(html).toContain('text-warning-fg')
    expect(html).toContain('border-stroke')
  })
})

describe('ConfirmDialog', () => {
  const props = {
    title: 'Delete item',
    confirmLabel: 'Delete',
    cancelLabel: 'Cancel',
    onConfirm: () => {},
    onCancel: () => {},
    children: 'Are you sure?',
  }

  it('renders nothing when closed', () => {
    const html = renderToStaticMarkup(createElement(ConfirmDialog, { ...props, open: false }))
    expect(html).toBe('')
  })

  it('renders a labelled modal dialog when open', () => {
    const html = renderToStaticMarkup(createElement(ConfirmDialog, { ...props, open: true }))
    expect(html).toContain('role="dialog"')
    expect(html).toContain('aria-modal="true"')
    expect(html).toContain('aria-labelledby=')
    expect(html).toContain('Delete item')
    expect(html).toContain('Are you sure?')
    expect(html).toContain('Cancel')
    expect(html).toContain('Delete')
  })

  it('disables actions and marks the confirmation busy while pending', () => {
    const html = renderToStaticMarkup(
      createElement(ConfirmDialog, { ...props, open: true, busy: true, destructive: true }),
    )
    expect(html).toContain('aria-busy="true"')
    expect(html.match(/disabled=""/g)?.length).toBeGreaterThanOrEqual(2)
  })
})
