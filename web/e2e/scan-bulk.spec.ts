import { expect, test, type Locator, type Page } from '@playwright/test'

/**
 * End-to-end smoke tests for the scan → buffer → bulk flow and the label
 * generator.
 *
 * The Go backend is not required: every `**\/api/v1/**` request is mocked, and
 * the camera is provided by a fake `getUserMedia` fixture (canvas capture
 * stream) so the tests are deterministic and run headless.
 */

const USER = {
  id: 1,
  username: 'admin',
  display_name: 'Admin',
  email: 'admin@example.com',
  role: 'admin',
  must_change_password: false,
  active: true,
  last_login_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const ITEM = {
  id: 1,
  asset_tag: 'PV-001',
  name: 'Test Camera',
  sn: 'SN-1',
  type: 'Camera',
  type_label: 'Camera',
  status: 'Feasibility',
  status_label: 'Feasibility',
  status_color: '#93c47d',
  sent_date: null as string | null,
  return_date: null as string | null,
  note: '',
  version: 1,
  archived: false,
  label_printed_at: null as string | null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  created_by: 'admin',
  updated_by: 'admin',
}

/** Settings fixture for the admin settings screen. */
const SETTINGS = {
  discord: { webhook_set: false, webhook_preview: '', enabled: false },
  scheduler: { check_hour: 11, days_ahead: 0 },
  locale: { default: 'en' },
  general: { organization_name: 'Sklad Test Org' },
}

/** User list fixture for the admin users screen (includes an inactive account). */
const USERS = [
  USER,
  { ...USER, id: 2, username: 'editor', display_name: 'Editor', email: 'editor@example.com', role: 'editor' },
  { ...USER, id: 3, username: 'viewer', display_name: 'Viewer', email: 'viewer@example.com', role: 'viewer', active: false },
]

/** Notification delivery fixture for the settings delivery log. */
const DELIVERY = {
  id: 1,
  channel_id: 1,
  trigger: 'scheduler',
  status: 'sent',
  item_count: 2,
  error: null,
  created_at: '2026-01-01T00:00:00Z',
  sent_at: '2026-01-01T00:00:00Z',
}

/** Pin the UI language so string assertions are stable. */
async function useEnglish(page: Page): Promise<void> {
  await page.addInitScript(() => {
    window.localStorage.setItem('sklad.lang', 'en')
  })
}

/** Deterministic fake camera: `getUserMedia` returns an animated canvas stream. */
async function installFakeCamera(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 640
    canvas.height = 480
    const context = canvas.getContext('2d')
    let frame = 0
    const draw = (): void => {
      if (context) {
        context.fillStyle = '#ffffff'
        context.fillRect(0, 0, canvas.width, canvas.height)
        context.fillStyle = '#000000'
        for (let index = 0; index < 16; index += 1) {
          context.fillRect(((index * 37) + frame) % canvas.width, 140, 8, 200)
        }
      }
      frame = (frame + 4) % canvas.width
      window.requestAnimationFrame(draw)
    }
    draw()

    const stream = canvas.captureStream(15)
    if (navigator.mediaDevices) {
      navigator.mediaDevices.getUserMedia = async () => stream
    } else {
      Object.defineProperty(navigator, 'mediaDevices', {
        configurable: true,
        value: { getUserMedia: async () => stream },
      })
    }
  })
}

interface MockApiOptions {
  /** Override the authenticated user; `null` simulates an anonymous session. */
  user?: Record<string, unknown> | null
  /** Override the initial item list returned by `GET /items`. */
  items?: Array<Record<string, unknown>>
}

/** Mock the whole `/api/v1` surface used by these specs. */
async function mockApi(page: Page, options: MockApiOptions = {}): Promise<void> {
  const currentUser = options.user !== undefined ? options.user : USER
  let items = (options.items ?? [ITEM]).map((entry) => ({ ...entry }))

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const { pathname } = new URL(request.url())
    const path = pathname.replace(/^\/api\/v1/, '')
    const method = request.method()

    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

    if (method === 'GET' && path === '/auth/me') {
      if (!currentUser) return json({ error: { code: 'unauthorized', message: 'Unauthorized' } }, 401)
      return json({ data: currentUser })
    }

    if (method === 'GET' && path === '/personnel') return json({data:[{id:1,name:'Customer',is_customer:true,active:true,version:1}]})
    if (method === 'GET' && path === '/version') {
      return json({ version: '1.0.0-test', commit: 'abcdef1234567890', build_time: '2026-01-01T12:00:00Z' })
    }

    if (method === 'GET' && path === '/options') {
      return json({
        data: {
          types: [{ value: 'Camera', label: 'Camera' }],
          statuses: [
            { value: 'Feasibility', label: 'Feasibility', color: '#93c47d' },
            { value: 'Customer', label: 'Customer On-Site', color: '#ffd966' },
          ],
        },
      })
    }

    if (method === 'GET' && path === '/settings') return json({ data: SETTINGS })
    if (method === 'PUT' && path === '/settings') return json({ data: SETTINGS })
    if (method === 'GET' && path === '/users') return json({ data: USERS })
    if (method === 'GET' && path === '/notifications') {
      return json({ data: [DELIVERY], meta: { page: 1, per_page: 25, total: 1, total_pages: 1 } })
    }

    if (method === 'GET' && path === '/items') {
      return json({ data: items, meta: { page: 1, per_page: 20, total: items.length, total_pages: 1 } })
    }

    if (method === 'POST' && path === '/items/resolve') {
      const { codes } = request.postDataJSON() as { codes: string[] }
      const matched = codes
        .map((code) => code.toUpperCase())
        .filter((code) => code.startsWith('PV-'))
        .map((code) => ({ ...ITEM, asset_tag: code }))
      const unknown = codes
        .map((code) => code.toUpperCase())
        .filter((code) => !code.startsWith('PV-'))
      return json({ data: { matched, unknown } })
    }

    if (method === 'POST' && path === '/items') {
      const body = request.postDataJSON() as Record<string, unknown>
      return json({ data: { ...ITEM, id: 2, asset_tag: (body.asset_tag as string) ?? 'NEW-1', name: (body.name as string) ?? '' } })
    }

    if (method === 'PATCH' && path.startsWith('/items/')) {
      const id = Number(path.slice('/items/'.length))
      const body = request.postDataJSON() as Record<string, unknown>
      items = items.map((entry) =>
        entry.id === id
          ? { ...entry, ...body, version: (Number(body.version) || Number(entry.version) || 1) + 1 }
          : entry,
      )
      return json({ data: items.find((entry) => entry.id === id) ?? ITEM })
    }

    if (method === 'DELETE' && path.startsWith('/items/')) {
      return json({ data: {} })
    }

    if (method === 'POST' && path === '/items/bulk') {
      const { items: bulkItems, patch } = request.postDataJSON() as {
        items: Array<{ asset_tag: string; version: number }>
        patch: Record<string, unknown>
      }
      const updated_items = bulkItems.map((entry) => ({
        ...ITEM,
        asset_tag: entry.asset_tag,
        version: entry.version + 1,
        ...(typeof patch.status === 'string' ? { status: patch.status } : {}),
      }))
      return json({ data: { updated: updated_items.length, updated_items } })
    }

    return json({ error: { code: 'not_found', message: `Unmocked ${method} ${path}` } }, 404)
  })
}

/**
 * Press Tab until `document.activeElement` satisfies `predicate` (using
 * keyboard navigation so the `:focus-visible` heuristic engages).
 */
async function tabUntilFocused(
  page: Page,
  predicate: () => boolean,
  maxTabs = 80,
): Promise<boolean> {
  for (let index = 0; index < maxTabs; index += 1) {
    await page.keyboard.press('Tab')
    if (await page.evaluate(predicate)) return true
  }
  return false
}

/**
 * Reach `locator` using only the Tab key (proves real keyboard reachability),
 * then assert focus landed on it.
 */
async function tabToTest(locator: Locator): Promise<void> {
  const page = locator.page()
  for (let index = 0; index < 400; index += 1) {
    if (await locator.evaluate((el) => el === document.activeElement)) return
    await page.keyboard.press('Tab')
  }
  throw new Error('element was not reachable via the Tab key')
}

/** Assert the global 2px/2px action-colored focus ring is visible. */
async function expectVisibleFocus(locator: Locator): Promise<void> {
  await expect(locator).toHaveCSS('outline-style', 'solid')
  await expect(locator).toHaveCSS('outline-width', '2px')
  await expect(locator).toHaveCSS('outline-offset', '2px')
  expect(await locator.evaluate((el) => getComputedStyle(el).outlineColor)).toBe('rgb(120, 190, 90)')
}

/** Assert the shared control ring color *and* its 2px width + 2px offset geometry. */
async function expectActionRing(locator: Locator): Promise<void> {
  const shadow = await locator.evaluate((el) => getComputedStyle(el).boxShadow)
  expect(shadow).toContain('rgb(120, 190, 90)')
  expect(shadow).toContain('0px 0px 0px 2px') // ring offset
  expect(shadow).toContain('0px 0px 0px 4px') // 2px ring + 2px offset
}

/**
 * Assert a keyboard focus indicator is visible on any control: either the
 * global 2px outline (links, raw buttons, checkboxes) or the shared control
 * ring (Input/Select/Button, which suppress the outline).
 */
async function expectAnyKeyboardFocus(locator: Locator): Promise<void> {
  const styles = await locator.evaluate((el) => {
    const style = getComputedStyle(el)
    return { outlineStyle: style.outlineStyle, outlineWidth: style.outlineWidth, boxShadow: style.boxShadow }
  })
  const visibleOutline = styles.outlineStyle === 'solid' && Number.parseFloat(styles.outlineWidth) >= 2
  const visibleRing = styles.boxShadow !== 'none' && styles.boxShadow.includes('0px 0px 0px 4px')
  expect(visibleOutline || visibleRing).toBe(true)
}

/**
 * WCAG 2.1 contrast ratio between an element's computed text colour and its
 * nearest opaque background. Optionally reads a pseudo-element (e.g. the
 * native `::placeholder`).
 */
async function computedContrast(locator: Locator, pseudo?: '::placeholder'): Promise<number> {
  return locator.evaluate((el, pseudoElement) => {
    const parse = (value: string): [number, number, number] => {
      const nums = value.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0]
      return [nums[0] ?? 0, nums[1] ?? 0, nums[2] ?? 0]
    }
    const channel = (value: number): number => {
      const scaled = value / 255
      return scaled <= 0.03928 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4
    }
    const luminance = ([red, green, blue]: [number, number, number]): number =>
      0.2126 * channel(red) + 0.7152 * channel(green) + 0.0722 * channel(blue)

    const foreground = parse(getComputedStyle(el, pseudoElement ?? null).color)

    let node: Element | null = el
    let background: [number, number, number] = [255, 255, 255]
    while (node) {
      const value = getComputedStyle(node).backgroundColor
      const nums = value.match(/[\d.]+/g)?.map(Number) ?? []
      if (nums.length >= 3 && (nums.length < 4 || nums[3] > 0)) {
        background = [nums[0], nums[1], nums[2]]
        break
      }
      node = node.parentElement
    }

    const first = luminance(foreground)
    const second = luminance(background)
    const lighter = Math.max(first, second)
    const darker = Math.min(first, second)
    return (lighter + 0.05) / (darker + 0.05)
  }, pseudo)
}

/** Page-level horizontal overflow in pixels (no contained-scroll allowance). */
async function measurePageOverflow(page: Page): Promise<number> {
  return page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
}

test.describe('scan → buffer → bulk', () => {
  test.beforeEach(async ({ page }) => {
    await useEnglish(page)
    await mockApi(page)
  })

  test('starts and stops the camera using the fake camera fixture', async ({ page }) => {
    await installFakeCamera(page)
    await page.goto('/scan')

    await page.getByRole('button', { name: 'Start camera' }).click()
    await expect(page.getByText('Scanning…')).toBeVisible()

    await page.getByRole('button', { name: 'Stop camera' }).click()
    await expect(page.getByRole('button', { name: 'Start camera' })).toBeVisible()
  })

  

  test('uses the Phase 1 primary color and shared control focus tokens', async ({ page }) => {
    await installFakeCamera(page)
    await page.goto('/scan')

    const start = page.getByRole('button', { name: 'Start camera' })
    await expect(start).toBeVisible()
    await expect(start).toHaveCSS('background-color', 'rgb(120, 190, 90)')
    await start.hover()
    await expect(start).toHaveCSS('background-color', 'rgb(145, 206, 119)')

    await page.getByLabel('Enter code manually').fill('PV-001')
    await page.getByRole('button', { name: 'Add to buffer' }).click()
    await expect(page.getByText('Matched: PV-001')).toBeVisible()

    const input = page.getByLabel('Enter code manually')
    await input.focus()
    await expect(input).toHaveCSS('border-radius', '6px')
    expect(await input.evaluate((el) => getComputedStyle(el).boxShadow)).toContain('rgb(120, 190, 90)')

    const select = page.getByRole('region', {name:'Rental buffer'}).getByLabel('Status', {exact:true})
    await select.focus()
    await expect(select).toHaveCSS('border-radius', '6px')
    expect(await select.evaluate((el) => getComputedStyle(el).boxShadow)).toContain('rgb(120, 190, 90)')

    const remove = page.getByRole('button', { name: 'Remove' })
    await expect(remove).toBeVisible()
    const focused = await tabUntilFocused(page, () => {
      const el = document.activeElement
      return el instanceof HTMLElement && el.getAttribute('aria-label') === 'Remove PV-001'
    })
    expect(focused).toBe(true)
    await expect(remove).toHaveCSS('outline-style', 'solid')
    await expect(remove).toHaveCSS('outline-width', '2px')
    await expect(remove).toHaveCSS('outline-offset', '2px')
  })
})

test.describe('labels', () => {
  test.beforeEach(async ({ page }) => {
    await useEnglish(page)
    await mockApi(page)
  })

  test('finds an asset for manual entry on the PT-E300', async ({ page }) => {
    await page.goto('/labels')
    await page.getByLabel('Search', { exact: true }).fill('PV-001')
    await page.getByRole('button', { name: /PV-001/ }).click()
    await expect(page.getByRole('region', { name: 'Enter on the printer' })).toContainText('PV-001')
    await expect(page.getByText('Enter the ID exactly, including hyphens and leading zeros.')).toBeVisible()
  })

})

test.describe('phase 1 foundation', () => {
  test.beforeEach(async ({ page }) => {
    await useEnglish(page)
    await mockApi(page)
  })

  test('honors reduced motion while keeping the dark color scheme and dark canvas', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    // `/` renders the inventory table, which has no fade-in element; `/about`
    // is an authenticated shell route that uses the shared `.animate-fade-in`
    // card and the navigation links this test asserts on.
    await page.goto('/about')

    const fadeIn = page.locator('.animate-fade-in').first()
    await expect(fadeIn).toBeVisible()
    expect(await fadeIn.evaluate((el) => getComputedStyle(el).animationName)).toBe('none')

    const navLink = page.locator('nav a').first()
    await expect(navLink).toBeVisible()
    expect(await navLink.evaluate((el) => getComputedStyle(el).transitionDuration)).toBe('0s')

    // The `/` index route has no fade-in card, but reduced motion, the light
    // color scheme and the dark canvas must hold there too.
    await page.goto('/')
    const homeNavLink = page.locator('nav a').first()
    await expect(homeNavLink).toBeVisible()
    expect(await homeNavLink.evaluate((el) => getComputedStyle(el).transitionDuration)).toBe('0s')
    expect(
      await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme),
    ).toBe('dark')
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe('rgb(16, 24, 32)')
  })

  test('shows a visible global focus indicator on headers, glyph actions and swatches', async ({ page }) => {
    await page.goto('/')

    const sortButton = page.getByRole('button', { name: 'Name' })
    await expect(sortButton).toBeVisible()
    await tabToTest(sortButton)
    await expectVisibleFocus(sortButton)

    const glyphButton = page.getByRole('button', { name: 'Edit' })
    await expect(glyphButton).toBeVisible()
    await tabToTest(glyphButton)
    await expectVisibleFocus(glyphButton)

    await page.goto('/options')
    const swatch = page.getByRole('button', { name: '#93c47d' }).first()
    await expect(swatch).toBeVisible()
    await tabToTest(swatch)
    await expectVisibleFocus(swatch)
  })

  test('uses the shared control geometry and focus ring for Input and Select', async ({ page }) => {
    await page.goto('/scan')

    const input = page.getByLabel('Enter code manually')
    await expect(input).toBeVisible()
    await expect(input).toHaveCSS('border-radius', '6px')
    expect(await input.evaluate((el) => getComputedStyle(el).borderTopColor)).toBe('rgb(101, 124, 140)')
    await input.focus()
    await expectActionRing(input)

    await input.fill('PV-001')
    await page.getByRole('button', { name: 'Add to buffer' }).click()
    await expect(page.getByText('Matched: PV-001')).toBeVisible()

    const select = page.getByRole('region', {name:'Rental buffer'}).getByLabel('Status', {exact:true})
    await expect(select).toBeVisible()
    await expect(select).toHaveCSS('border-radius', '6px')
    expect(await select.evaluate((el) => getComputedStyle(el).borderTopColor)).toBe('rgb(101, 124, 140)')
    await select.focus()
    await expectActionRing(select)
  })

  test('renders invalid Input and Select in the browser with danger borders and rings', async ({ page }) => {
    await page.goto('/e2e/fixtures/invalid-controls.html')

    const invalidInput = page.getByLabel('invalid input')
    await expect(invalidInput).toBeVisible()
    await expect(invalidInput).toHaveAttribute('aria-invalid', 'true')
    await expect(invalidInput).toHaveCSS('border-radius', '6px')
    expect(await invalidInput.evaluate((el) => getComputedStyle(el).borderTopColor)).toBe('rgb(245, 170, 167)')
    await invalidInput.focus()
    const inputShadow = await invalidInput.evaluate((el) => getComputedStyle(el).boxShadow)
    expect(inputShadow).toContain('rgb(245, 170, 167)')
    expect(inputShadow).toContain('0px 0px 0px 2px')
    expect(inputShadow).toContain('0px 0px 0px 4px')

    const invalidSelect = page.getByLabel('invalid select')
    await expect(invalidSelect).toBeVisible()
    await expect(invalidSelect).toHaveAttribute('aria-invalid', 'true')
    await expect(invalidSelect).toHaveCSS('border-radius', '6px')
    expect(await invalidSelect.evaluate((el) => getComputedStyle(el).borderTopColor)).toBe('rgb(245, 170, 167)')
    await invalidSelect.focus()
    const selectShadow = await invalidSelect.evaluate((el) => getComputedStyle(el).boxShadow)
    expect(selectShadow).toContain('rgb(245, 170, 167)')
    expect(selectShadow).toContain('0px 0px 0px 2px')
    expect(selectShadow).toContain('0px 0px 0px 4px')

    // The native date input is styled by a `:where(...)` base rule so the
    // `invalid` utility still wins; this proves the specificity fix.
    const invalidDate = page.getByLabel('invalid date')
    await expect(invalidDate).toBeVisible()
    await expect(invalidDate).toHaveAttribute('aria-invalid', 'true')
    await expect(invalidDate).toHaveCSS('border-radius', '6px')
    expect(await invalidDate.evaluate((el) => getComputedStyle(el).borderTopColor)).toBe('rgb(245, 170, 167)')
    await invalidDate.focus()
    const dateShadow = await invalidDate.evaluate((el) => getComputedStyle(el).boxShadow)
    expect(dateShadow).toContain('rgb(245, 170, 167)')
    expect(dateShadow).toContain('0px 0px 0px 2px')
    expect(dateShadow).toContain('0px 0px 0px 4px')
  })

  test('keeps the dark canvas and dark color scheme at desktop and mobile widths', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto('/')
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe('rgb(16, 24, 32)')
    expect(
      await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme),
    ).toBe('dark')

    await page.setViewportSize({ width: 360, height: 800 })
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe('rgb(16, 24, 32)')
    expect(
      await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme),
    ).toBe('dark')
  })
})

test.describe('phase 3 consolidation', () => {
  test.beforeEach(async ({ page }) => {
    await useEnglish(page)
    await mockApi(page)
  })

  test('shows the dark shell, a neutral language switcher, a skip link and 44px icon actions', async ({ page }) => {
    await page.goto('/')

    await expect(page.locator('header')).toHaveCSS('background-color', 'rgb(24, 35, 45)')

    // Skip link: the first Tab reaches it, Enter moves focus to <main>.
    await page.keyboard.press('Tab')
    const skip = page.getByRole('link', { name: 'Skip to content' })
    await expect(skip).toBeFocused()
    await skip.press('Enter')
    await expect(page.locator('#main-content')).toBeFocused()

    const switcher = page.getByLabel('Language')
    await expect(switcher).toBeVisible()
    await expect(switcher).toHaveCSS('border-radius', '6px')
    expect(await switcher.evaluate((el) => getComputedStyle(el).borderTopColor)).toBe('rgb(101, 124, 140)')

    const edit = page.getByRole('button', { name: 'Edit' })
    const box = await edit.boundingBox()
    expect(box?.width ?? 0).toBeGreaterThanOrEqual(44)
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(44)

    // Locale handling keeps <html lang> in sync; dark browser metadata.
    await switcher.selectOption('cs')
    await expect(page.locator('html')).toHaveAttribute('lang', 'cs')
    expect(await page.locator('meta[name="color-scheme"]').getAttribute('content')).toBe('dark')
    expect(await page.locator('meta[name="theme-color"]').getAttribute('content')).toBe('#101820')
  })

  test('toggles sort semantics and edits/cancels an inventory row', async ({ page }) => {
    await page.goto('/')

    const idHeader = page.locator('th', { has: page.getByRole('button', { name: 'ID' }) })
    await expect(idHeader).toHaveAttribute('aria-sort', 'ascending')
    const nameHeader = page.locator('th', { has: page.getByRole('button', { name: 'Name' }) })
    await expect(nameHeader).toHaveAttribute('aria-sort', 'none')

    // Toggling a new column moves the active sort and switches direction on a
    // second activation.
    await page.getByRole('button', { name: 'Name' }).click()
    await expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
    await expect(idHeader).toHaveAttribute('aria-sort', 'none')
    await page.getByRole('button', { name: 'Name' }).click()
    await expect(nameHeader).toHaveAttribute('aria-sort', 'descending')

    // Inline edit → Cancel leaves the row untouched.
    await page.getByRole('button', { name: 'Edit' }).click()
    const nameInput = page.getByLabel('Item name')
    await expect(nameInput).toBeVisible()
    await nameInput.fill('Discarded change')
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(nameInput).toBeHidden()
    await expect(page.getByText('Test Camera')).toBeVisible()

    // Inline edit → Save persists the exact payload and closes the editor.
    await page.getByRole('button', { name: 'Edit' }).click()
    await page.getByLabel('Item name').fill('Edited Camera')
    const patchRequest = page.waitForRequest(
      (request) => request.method() === 'PATCH' && new URL(request.url()).pathname === '/api/v1/items/1',
    )
    await page.getByRole('button', { name: 'Save' }).click()
    const patch = await patchRequest
    expect(patch.postDataJSON()).toEqual({
      version: 1,
      name: 'Edited Camera',
      sn: 'SN-1',
      type: 'Camera',
    })
    await expect(page.getByText('Item saved.')).toBeVisible()
    await expect(page.getByLabel('Item name')).toBeHidden()

    // The mocked PATCH persists across refetches, so the saved value survives
    // Refresh and reopening the editor (independent of any real backend).
    await expect(page.getByRole('cell', { name: 'Edited Camera' })).toBeVisible()
    const refreshResponse = page.waitForResponse(
      (response) => response.request().method() === 'GET' && new URL(response.url()).pathname === '/api/v1/items',
    )
    await page.getByRole('button', { name: 'Refresh' }).click()
    await refreshResponse
    await expect(page.getByRole('cell', { name: 'Edited Camera' })).toBeVisible()
    await page.getByRole('button', { name: 'Edit' }).click()
    await expect(page.getByLabel('Item name')).toHaveValue('Edited Camera')
    await page.getByRole('button', { name: 'Cancel' }).click()
  })

  test('shows the new-entry buffer row with associated fields', async ({ page }) => {
    await page.goto('/')

    await page.getByRole('button', { name: 'Add new entry' }).click()
    const bufferHeading = page.getByRole('heading', { name: 'New entries (unsaved)' })
    await expect(bufferHeading).toBeVisible()

    const nameInput = page.getByLabel('Item name')
    await expect(nameInput).toBeVisible()
    await nameInput.fill('Buffer item')
    await expect(page.getByRole('button', { name: 'Save' })).toBeEnabled()

    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(bufferHeading).toBeHidden()
  })

  test('contains inventory horizontal scroll without page overflow', async ({ page }) => {
    for (const width of [360, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/')
      // Wait for the table to exist before measuring, so the overflow check
      // cannot pass against a not-yet-rendered scrolling region.
      await expect(page.getByRole('button', { name: 'ID' })).toBeVisible()
      await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)

      // The new-entry buffer table must also scroll internally, not the page.
      await page.getByRole('button', { name: 'Add new entry' }).click()
      await expect(page.getByRole('heading', { name: 'New entries (unsaved)' })).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
      await page.getByRole('button', { name: 'Cancel' }).click()

      // Likewise the inline editor row.
      await page.getByRole('button', { name: 'Edit' }).click()
      await expect(page.getByLabel('Item name')).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
      await page.getByRole('button', { name: 'Cancel' }).click()
    }
  })

  test('archive dialog is labelled, focuses Cancel, traps focus and restores on Escape', async ({ page }) => {
    await page.goto('/')
    const opener = page.getByRole('button', { name: 'Archive' }).first()
    await opener.click()

    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog).toHaveAttribute('aria-modal', 'true')
    await expect(dialog.getByRole('heading', { name: 'Archive' })).toBeVisible()

    const cancel = dialog.getByRole('button', { name: 'Cancel' })
    const confirm = dialog.getByRole('button', { name: 'Archive' })
    await expect(cancel).toBeFocused()

    // Programmatic focus on an outside control is pulled back inside.
    await page.getByRole('button', { name: 'Refresh' }).focus()
    await expect(cancel).toBeFocused()

    await confirm.focus()
    await page.keyboard.press('Tab')
    await expect(cancel).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(confirm).toBeFocused()

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(opener).toBeFocused()
  })

  test('keeps a pending archive dialog open, guarded and single-shot', async ({ page }) => {
    let deleteRequests = 0
    let release: (() => void) | null = null
    await page.route('**/api/v1/items/1', async (route) => {
      if (route.request().method() === 'DELETE') {
        deleteRequests += 1
        await new Promise<void>((resolve) => {
          release = resolve
        })
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: {} }) })
      }
      return route.fallback()
    })

    await page.goto('/')
    await page.getByRole('button', { name: 'Archive' }).first().click()
    const dialog = page.getByRole('dialog')
    const cancel = dialog.getByRole('button', { name: 'Cancel' })
    const confirm = dialog.getByRole('button', { name: 'Archive' })

    await confirm.click()
    await expect(confirm).toBeDisabled()
    await expect(cancel).toBeDisabled()
    // With every action disabled, focus is contained on the dialog itself.
    await expect(dialog).toBeFocused()

    await page.keyboard.press('Escape')
    await expect(dialog).toBeVisible()

    // A forced activation while pending must not issue a second request.
    await confirm.dispatchEvent('click')
    expect(deleteRequests).toBe(1)

    release?.()
    await expect(dialog).toBeHidden()
  })

  test('archive success rescues focus to the fallback when the row disappears later', async ({ page }) => {
    let archived = false
    let releaseDelete: (() => void) | undefined
    let releaseList: (() => void) | undefined

    await page.route('**/api/v1/items**', async (route) => {
      const url = new URL(route.request().url())
      if (archived && route.request().method() === 'GET' && url.pathname === '/api/v1/items') {
        await new Promise<void>((resolve) => {
          releaseList = resolve
        })
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: [], meta: { page: 1, per_page: 20, total: 0, total_pages: 1 } }),
        })
      }
      return route.fallback()
    })
    await page.route('**/api/v1/items/1', async (route) => {
      if (route.request().method() !== 'DELETE') return route.fallback()
      archived = true
      await new Promise<void>((resolve) => {
        releaseDelete = resolve
      })
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: {} }) })
    })

    await page.goto('/')
    await page.getByRole('button', { name: 'Archive', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Archive', exact: true }).click()
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeDisabled()

    await expect.poll(() => typeof releaseDelete).toBe('function')
    releaseDelete?.()
    await expect(dialog).toBeHidden()

    // The invalidated list refetch now removes the archived row (held until
    // released). Focus must not fall to <body>; it lands on Refresh.
    await expect.poll(() => typeof releaseList).toBe('function')
    releaseList?.()
    await expect(page.getByRole('button', { name: 'Edit' })).toBeHidden()
    await expect(page.getByRole('button', { name: 'Refresh' })).toBeFocused()
  })

  test('does not steal focus back after the user moves on following archive', async ({ page }) => {
    let archived = false
    let releaseList: (() => void) | undefined
    await page.route('**/api/v1/items**', async (route) => {
      const url = new URL(route.request().url())
      if (archived && route.request().method() === 'GET' && url.pathname === '/api/v1/items') {
        await new Promise<void>((resolve) => {
          releaseList = resolve
        })
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: [], meta: { page: 1, per_page: 20, total: 0, total_pages: 1 } }),
        })
      }
      return route.fallback()
    })
    await page.route('**/api/v1/items/1', async (route) => {
      if (route.request().method() !== 'DELETE') return route.fallback()
      archived = true
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: {} }) })
    })

    await page.goto('/')
    await page.getByRole('button', { name: 'Archive', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Archive', exact: true }).click()
    await page.getByRole('dialog').waitFor({ state: 'hidden' })

    // The user deliberately moves focus away before the refetch settles.
    const search = page.getByLabel('Search', { exact: true })
    await search.focus()
    await expect(search).toBeFocused()

    await expect.poll(() => typeof releaseList).toBe('function')
    releaseList?.()
    await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeHidden()

    // The removal of the archived row must not steal focus from Search.
    await expect(search).toBeFocused()
  })

  

  
})

test.describe('phase 4 remaining screens & verification', () => {
  test.beforeEach(async ({ page }) => {
    await useEnglish(page)
    await mockApi(page)
  })

  test('all authenticated routes render without page-level overflow at 360/768/1440', async ({ page }) => {
    test.slow()
    const routes: Array<{ path: string; ready: (page: Page) => Locator }> = [
      { path: '/', ready: (page) => page.getByRole('button', { name: 'ID' }) },
      { path: '/scan', ready: (page) => page.getByRole('button', { name: 'Start camera' }) },
      { path: '/labels', ready: (page) => page.getByRole('heading', { name: 'Labels' }) },
      { path: '/options', ready: (page) => page.getByRole('heading', { name: 'Manage options' }) },
      { path: '/settings', ready: (page) => page.getByRole('heading', { name: 'Settings' }) },
      { path: '/users', ready: (page) => page.getByRole('heading', { name: 'Accounts' }) },
      { path: '/audit', ready: (page) => page.getByRole('heading', { name: 'Audit log' }) },
      { path: '/about', ready: (page) => page.getByRole('heading', { name: 'About' }) },
      { path: '/change-password', ready: (page) => page.getByRole('heading', { name: 'Change password' }) },
      { path: '/definitely-not-a-route', ready: (page) => page.getByText('Not found.') },
    ]

    for (const width of [360, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      for (const route of routes) {
        await page.goto(route.path)
        await expect(route.ready(page)).toBeVisible()
        expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
      }
    }
  })

  test('renders each locale catalog with nav destinations intact and no overflow', async ({ page }) => {
    await page.setViewportSize({ width: 768, height: 900 })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Inventory' })).toBeVisible()
    expect(await page.locator('nav a').count()).toBe(8)

    const switcher = page.locator('header select')
    await switcher.selectOption('cs')
    await expect(page.locator('html')).toHaveAttribute('lang', 'cs')
    await expect(page.getByRole('heading', { name: 'Sklad' })).toBeVisible()
    expect(await page.locator('nav a').count()).toBe(8)
    expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)

    await switcher.selectOption('it')
    await expect(page.locator('html')).toHaveAttribute('lang', 'it')
    await expect(page.getByRole('heading', { name: 'Inventario' })).toBeVisible()
    expect(await page.locator('nav a').count()).toBe(8)
    expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)

    await switcher.selectOption('en')
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
    await expect(page.getByRole('heading', { name: 'Inventory' })).toBeVisible()
  })

  test('long item and account names do not cause page-level overflow', async ({ page }) => {
    const longName = 'LongInventoryItemNameWithoutSpaces'.repeat(4)
    await page.route('**/api/v1/items**', async (route) => {
      const url = new URL(route.request().url())
      if (route.request().method() === 'GET' && url.pathname === '/api/v1/items') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [{ ...ITEM, name: longName }],
            meta: { page: 1, per_page: 20, total: 1, total_pages: 1 },
          }),
        })
      }
      return route.fallback()
    })
    await page.route('**/api/v1/auth/me', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { ...USER, display_name: longName } }),
      }),
    )

    for (const width of [360, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/')
      await expect(page.getByRole('button', { name: 'ID' })).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
    }
  })

  test('populated long-content states do not overflow at 360/768/1440', async ({ page }) => {
    test.slow()
    const longName = 'LongInventoryItemNameWithoutSpaces'.repeat(4)
    const longMetadata = 'unknown'.repeat(20)

    await page.route('**/api/v1/items**', async (route) => {
      const url = new URL(route.request().url())
      if (route.request().method() === 'GET' && url.pathname === '/api/v1/items') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [{ ...ITEM, name: longName }],
            meta: { page: 1, per_page: 20, total: 1000, total_pages: 50 },
          }),
        })
      }
      return route.fallback()
    })
    await page.route('**/api/v1/items/resolve', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { matched: [{ ...ITEM, name: longName }], unknown: [] } }),
      }),
    )
    await page.route('**/api/v1/version', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ version: 'v'.repeat(120), commit: 'c'.repeat(40), build_time: longMetadata }),
      }),
    )

    for (const width of [360, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })

      // Inventory: long item name and a realistic pagination count, in English
      // and Italian (locale coverage is not desktop-only).
      await page.goto('/')
      await expect(page.getByRole('button', { name: 'ID' })).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
      await page.locator('header select').selectOption('it')
      await expect(page.getByRole('heading', { name: 'Inventario' })).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)

      // Scan: a populated buffer whose item name has no break opportunities.
      await page.goto('/scan')
      await page.getByLabel('Enter code manually').fill('PV-001')
      await page.getByRole('button', { name: 'Add to buffer' }).click()
      await expect(page.getByText(longName, { exact: false }).first()).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)

      // About: long unbroken version/commit/build metadata.
      await page.goto('/about')
      await expect(page.getByRole('heading', { name: 'About' })).toBeVisible()
      expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
    }
  })

  test('bounds long asset tags and long status labels at every width', async ({ page }) => {
    test.slow()
    let longLabel = false
    await page.route((url) => url.pathname === '/api/v1/options', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            types: [],
            statuses: [{ value: 'Ready', label: longLabel ? 'S'.repeat(80) : 'Ready', color: '#93c47d' }],
          },
        }),
      }),
    )
    await page.route('**/api/v1/items/resolve', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            matched: [
              {
                ...ITEM,
                asset_tag: longLabel ? 'PV-001' : 'P'.repeat(64),
                name: 'Camera',
                status: 'Ready',
                status_label: longLabel ? 'S'.repeat(80) : 'Ready',
                status_color: '#93c47d',
              },
            ],
            unknown: [],
          },
        }),
      }),
    )

    for (const width of [360, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      for (const mode of ['long-tag', 'long-label']) {
        longLabel = mode === 'long-label'
        await page.goto('/scan')
        await page.getByLabel('Enter code manually').fill(longLabel ? 'PV-001' : 'P'.repeat(64))
        await page.getByRole('button', { name: 'Add to buffer' }).click()
        await expect(page.getByRole('button', { name: /^Remove /, exact: false })).toBeVisible()
        expect(await measurePageOverflow(page)).toBeLessThanOrEqual(1)
      }
    }
  })

  

  test('anonymous visitors can browse but administration requires login', async ({ page }) => {
    await page.route('**/api/v1/auth/me', (route) =>
      route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'unauthorized', message: 'x' } }),
      }),
    )
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Inventory' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Admin sign in', exact: true })).toBeVisible()
    await page.goto('/settings')
    await expect(page).toHaveURL(/\/login$/)
    await expect(page.getByRole('button', { name: 'Admin sign in' })).toBeVisible()
  })

  test('login associates labels and announces credential errors', async ({ page }) => {
    await page.route('**/api/v1/auth/me', (route) =>
      route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'unauthorized', message: 'x' } }),
      }),
    )
    await page.route('**/api/v1/auth/login', (route) =>
      route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'unauthorized', message: 'x' } }),
      }),
    )
    await page.goto('/login')

    const username = page.getByLabel('Username', { exact: true })
    const password = page.getByLabel('Password', { exact: true })
    await expect(username).toBeVisible()
    await expect(password).toBeVisible()
    await expect(username).toHaveAttribute('autocomplete', 'username')
    await expect(password).toHaveAttribute('type', 'password')

    await username.fill('admin')
    await password.fill('wrong')
    await page.getByRole('button', { name: 'Admin sign in' }).click()

    const alert = page.getByRole('alert')
    await expect(alert).toHaveText('Invalid username or password.')
    const describedBy = await username.getAttribute('aria-describedby')
    expect(describedBy).toBeTruthy()
    await expect(page.locator(`[id="${describedBy}"]`)).toHaveClass(/text-danger-fg/)
  })

  test('change-password associates labels and announces validation errors', async ({ page }) => {
    await page.goto('/change-password')
    await expect(page.getByRole('heading', { name: 'Change password' })).toBeVisible()

    const current = page.getByLabel('Current password', { exact: true })
    const next = page.getByLabel('New password', { exact: true })
    const confirm = page.getByLabel('Confirm new password', { exact: true })
    await expect(current).toBeVisible()
    await expect(next).toBeVisible()
    await expect(confirm).toBeVisible()
    await expect(current).toHaveAttribute('autocomplete', 'current-password')
    await expect(next).toHaveAttribute('minlength', '8')

    await current.fill('current1')
    await next.fill('password1')
    await confirm.fill('password2')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByRole('alert')).toHaveText('Passwords do not match.')
  })

  test('keyboard reaches controls with a visible focus indicator across screens', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('button', { name: 'ID' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Archive' })).toBeVisible()

    const search = page.getByLabel('Search', { exact: true })
    await tabToTest(search)
    await expectActionRing(search)

    const sort = page.getByRole('button', { name: 'ID' })
    await tabToTest(sort)
    await expectVisibleFocus(sort)

    const edit = page.getByRole('button', { name: 'Edit' }).first()
    await tabToTest(edit)
    await expectVisibleFocus(edit)

    const archived = page.getByLabel('Show archived', { exact: true })
    await tabToTest(archived)
    await expectAnyKeyboardFocus(archived)

    await page.goto('/scan')
    const manual = page.getByLabel('Enter code manually')
    await tabToTest(manual)
    await expectActionRing(manual)
    const start = page.getByRole('button', { name: 'Start camera' })
    await tabToTest(start)
    await expectAnyKeyboardFocus(start)

    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    const enabled = page.getByLabel('Enabled', { exact: true })
    await tabToTest(enabled)
    await expectAnyKeyboardFocus(enabled)
    const saveButton = page.getByRole('button', { name: 'Send test message' })
    await tabToTest(saveButton)
    await expectAnyKeyboardFocus(saveButton)
  })

  test('about shows a loading state then an error alert', async ({ page }) => {
    let release: (() => void) | undefined
    await page.route('**/api/v1/version', async (route) => {
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'server_error', message: 'boom' } }),
      })
    })

    await page.goto('/about')
    await expect(page.getByText('Loading…')).toBeVisible()
    await expect.poll(() => typeof release).toBe('function')
    release?.()
    await expect(page.getByRole('alert')).toContainText('Server error. Please try again.')
  })

  test('admin screens surface GET errors instead of loading forever or empty content', async ({ page }) => {
    const failure = {
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'server_error', message: 'boom' } }),
    }

    await page.route((url) => url.pathname === '/api/v1/options', (route) => route.fulfill(failure))
    await page.goto('/options')
    await expect(page.getByRole('alert')).toContainText('Server error. Please try again.')

    await page.route((url) => url.pathname === '/api/v1/settings', (route) => route.fulfill(failure))
    await page.goto('/settings')
    await expect(page.getByRole('alert')).toContainText('Server error. Please try again.')

    await page.route((url) => url.pathname === '/api/v1/personnel', (route) => route.fulfill(failure))
    await page.goto('/personnel')
    await expect(page.getByRole('alert')).toContainText('Could not load rental options.')
  })

  test('label search surfaces empty and error states', async ({ page }) => {
    let fail = false
    await page.route('**/api/v1/items**', async (route) => {
      const url = new URL(route.request().url())
      if (route.request().method() === 'GET' && url.pathname === '/api/v1/items') {
        if (fail) {
          return route.fulfill({
            status: 500,
            contentType: 'application/json',
            body: JSON.stringify({ error: { code: 'server_error', message: 'boom' } }),
          })
        }
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: [], meta: { page: 1, per_page: 20, total: 0, total_pages: 1 } }),
        })
      }
      return route.fallback()
    })

    await page.goto('/labels?tag=ZZZ-404')
    await expect(page.getByText('No items match your search.')).toBeVisible()

    fail = true
    await page.getByLabel('Search', { exact: true }).fill('ZZZ-500')
    await expect(page.getByRole('alert')).toContainText('Server error. Please try again.')
  })

  test('keeps measurable text contrast on actions, notices and API badges', async ({ page }) => {
    await page.route((url) => url.pathname === '/api/v1/options', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            types: [{ value: 'Camera', label: 'Camera' }],
            statuses: [
              { value: 'Feasibility', label: 'Feasibility', color: '#808080' },
              { value: 'Customer', label: 'Customer On-Site', color: '#ffd966' },
            ],
          },
        }),
      }),
    )
    await installFakeCamera(page)
    await page.goto('/scan')

    const start = page.getByRole('button', { name: 'Start camera' })
    await expect(start).toBeVisible()
    expect(await computedContrast(start)).toBeGreaterThanOrEqual(4.5)

    const hint = page.getByText('Hold the label across the green line, about 15–25 cm from the camera.').first()
    await expect(hint).toBeVisible()
    expect(await computedContrast(hint)).toBeGreaterThanOrEqual(4.5)

    const manual = page.getByLabel('Enter code manually')
    expect(await computedContrast(manual, '::placeholder')).toBeGreaterThanOrEqual(4.5)

    await manual.fill('PV-001')
    await page.getByRole('button', { name: 'Add to buffer' }).click()
    const success = page.getByRole('status').first()
    await expect(success).toContainText('Matched: PV-001')
    expect(await computedContrast(success)).toBeGreaterThanOrEqual(4.5)

    // info intent (unknown code)
    await manual.fill('ZZZ-UNKNOWN')
    await page.getByRole('button', { name: 'Add to buffer' }).click()
    const info = page.getByRole('status').first()
    await expect(info).toContainText('Unknown code')
    expect(await computedContrast(info)).toBeGreaterThanOrEqual(4.5)

    // error intent (resolve fails)
    await page.route('**/api/v1/items/resolve', (route) =>
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'server_error', message: 'boom' } }),
      }),
    )
    await manual.fill('ZZZ-ERROR')
    await page.getByRole('button', { name: 'Add to buffer' }).click()
    const errorNotice = page.getByRole('alert').first()
    await expect(errorNotice).toContainText('Server error. Please try again.')
    expect(await computedContrast(errorNotice)).toBeGreaterThanOrEqual(4.5)

    // Mid-luminance API colour: the chosen black foreground must stay legible.
    await page.goto('/')
    const badge = page.locator('span.rounded-full', { hasText: 'Feasibility' }).first()
    await expect(badge).toBeVisible()
    expect(await computedContrast(badge)).toBeGreaterThanOrEqual(4.5)

    await page.goto('/')
    await page.getByRole('button', { name: 'Archive' }).first().click()
    const danger = page.getByRole('dialog').getByRole('button', { name: 'Archive', exact: true })
    await expect(danger).toBeVisible()
    expect(await computedContrast(danger)).toBeGreaterThanOrEqual(4.5)
    await page.keyboard.press('Escape')

    // warning intent (forced password change)
    await page.route('**/api/v1/auth/me', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { ...USER, must_change_password: true } }),
      }),
    )
    await page.goto('/change-password')
    const warning = page.getByText('You must change your password before continuing.')
    await expect(warning).toBeVisible()
    expect(await computedContrast(warning)).toBeGreaterThanOrEqual(4.5)
  })

  test('shows manual PT-E300 instructions without unsupported print assets', async ({ page }) => {
    await page.goto('/labels?tag=PV-001')
    await expect(page.getByRole('region', { name: 'Enter on the printer' })).toContainText('PV-001')
    await expect(page.getByText('Press Bar Code and select CODE128.')).toBeVisible()
    await expect(page.getByRole('button', { name: /Print|Download SVG|Download PNG/ })).toHaveCount(0)
    await expect(page.getByLabel('Tape width', { exact: true })).toHaveCount(0)
    await page.getByRole('link', { name: 'Verify label in Scan' }).click()
    await expect(page.getByLabel('Enter code manually')).toBeVisible()
  })

  test('confirm dialog guards a synchronous double activation while enabled', async ({ page }) => {
    await page.goto('/e2e/fixtures/confirm-guard.html')
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    const confirm = dialog.getByRole('button', { name: 'Confirm' })
    await expect(confirm).toBeEnabled()

    const result = await confirm.evaluate((element) => {
      const button = element as HTMLButtonElement
      const enabledBefore = !button.disabled
      button.click()
      const enabledAfterFirst = !button.disabled
      button.click()
      const enabledAfterSecond = !button.disabled
      const calls = (window as unknown as { __confirmGuardCalls?: number }).__confirmGuardCalls ?? -1
      return { enabledBefore, enabledAfterFirst, enabledAfterSecond, calls }
    })

    expect(result.enabledBefore).toBe(true)
    expect(result.enabledAfterFirst).toBe(true)
    expect(result.enabledAfterSecond).toBe(true)
    expect(result.calls).toBe(1)
    expect(
      await page.evaluate(() => (window as unknown as { __confirmGuardCalls?: number }).__confirmGuardCalls),
    ).toBe(1)
  })
})

test('options protect references from active and archived equipment', async ({ page }) => {
  await useEnglish(page)
  await mockApi(page)
  await page.route((url) => url.pathname === '/api/v1/items', (route) => {
    const archived = new URL(route.request().url()).searchParams.get('archived') === 'true'
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      data: [{ ...ITEM, status: archived ? 'Customer' : 'Feasibility', archived }],
      meta: { page: 1, per_page: 1000, total: 1, total_pages: 1 },
    }) })
  })
  await page.goto('/options')
  await expect(page.getByRole('button', { name: 'In use', exact: true })).toHaveCount(3)
  for (const button of await page.getByRole('button', { name: 'In use', exact: true }).all()) {
    await expect(button).toBeDisabled()
  }
})




