import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { Input } from './Input'
import { Select } from './Select'

/**
 * Phase 1 shared-control tokens.
 *
 * No current screen passes `invalid`, so the invalid appearance cannot be
 * reached through the running UI. These tests render the components to static
 * markup and assert the exact class contract that Tailwind compiles into the
 * shared border/focus/validation styling.
 */
describe('shared control tokens', () => {
  it('renders neutral tokens for a valid Input', () => {
    const html = renderToStaticMarkup(createElement(Input, {}))
    expect(html).toContain('rounded-md')
    expect(html).toContain('border-stroke-control')
    expect(html).toContain('focus:border-action')
    expect(html).toContain('focus:ring-action')
    expect(html).toContain('focus:ring-offset-2')
    expect(html).not.toContain('border-danger-fg')
  })

  it('renders danger validation tokens for an invalid Input', () => {
    const html = renderToStaticMarkup(createElement(Input, { invalid: true }))
    expect(html).toContain('aria-invalid="true"')
    expect(html).toContain('rounded-md')
    expect(html).toContain('border-danger-fg')
    expect(html).toContain('focus:border-danger-fg')
    expect(html).toContain('focus:ring-danger-fg')
    expect(html).not.toContain('border-stroke-control')
    expect(html).not.toContain('focus:border-action')
  })

  it('renders neutral tokens for a valid Select', () => {
    const html = renderToStaticMarkup(createElement(Select, {}, 'x'))
    expect(html).toContain('rounded-md')
    expect(html).toContain('border-stroke-control')
    expect(html).toContain('focus:border-action')
    expect(html).toContain('focus:ring-action')
    expect(html).toContain('focus:ring-offset-2')
    expect(html).not.toContain('border-danger-fg')
  })

  it('renders danger validation tokens for an invalid Select', () => {
    const html = renderToStaticMarkup(createElement(Select, { invalid: true }, 'x'))
    expect(html).toContain('aria-invalid="true"')
    expect(html).toContain('rounded-md')
    expect(html).toContain('border-danger-fg')
    expect(html).toContain('focus:border-danger-fg')
    expect(html).toContain('focus:ring-danger-fg')
    expect(html).not.toContain('border-stroke-control')
    expect(html).not.toContain('focus:border-action')
  })

  it('uses semantic panel/content tokens instead of raw white/ink', () => {
    const input = renderToStaticMarkup(createElement(Input, { placeholder: 'x' }))
    expect(input).toContain('bg-panel')
    expect(input).toContain('text-content')
    expect(input).toContain('placeholder:text-content-muted')
    expect(input).not.toContain('bg-white')

    const select = renderToStaticMarkup(createElement(Select, {}, 'x'))
    expect(select).toContain('bg-panel')
    expect(select).toContain('text-content')
    expect(select).not.toContain('bg-white')
  })

  it('renders an invalid date control with the danger contract', () => {
    const html = renderToStaticMarkup(createElement(Input, { type: 'date', invalid: true }))
    expect(html).toContain('type="date"')
    expect(html).toContain('aria-invalid="true"')
    expect(html).toContain('border-danger-fg')
    expect(html).toContain('focus:ring-danger-fg')
  })
})
