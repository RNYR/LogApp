/*
 * PREVIEW-ONLY mock API for the Sklad UI-overhaul review.
 *
 * This file exists ONLY in the `sklad_revised/` snapshot so the revised UI can
 * be explored in a browser without the Go backend (no Go/Docker available in
 * this environment). It patches `window.fetch` for `/api/v1/*` with an
 * in-memory dataset. It is NOT part of the application and is not present in
 * the committed `sklad/` sources.
 */
(function () {
  'use strict'

  var API = '/api/v1'
  var origFetch = window.fetch.bind(window)

  // ---------------------------------------------------------------- fixtures --

  var STATUSES = [{"value": "Feasibility", "label": "Feasibility", "labels": {"en": "Feasibility", "cs": "Proveditelnost", "it": "Fattibilità"}, "color": "#93c47d", "sort_order": 0}, {"value": "Devel", "label": "Devel", "labels": {"en": "Devel", "cs": "Vývoj", "it": "Sviluppo"}, "color": "#8faadc", "sort_order": 1}, {"value": "Sales", "label": "Sales", "labels": {"en": "Sales", "cs": "Obchod", "it": "Vendite"}, "color": "#cc99ff", "sort_order": 2}, {"value": "Customer", "label": "Customer On-Site", "labels": {"en": "Customer On-Site", "cs": "U zákazníka", "it": "Presso cliente"}, "color": "#ffd966", "sort_order": 3}, {"value": "Loaned", "label": "Loaned", "labels": {"en": "Loaned", "cs": "Zapůjčeno", "it": "In prestito"}, "color": "#e06666", "sort_order": 4}]

  var TYPES = [{"value": "Camera", "label": "Camera", "labels": {"en": "Camera", "cs": "Kamera", "it": "Telecamera"}, "sort_order": 0}, {"value": "Lens", "label": "Lens", "labels": {"en": "Lens", "cs": "Objektiv", "it": "Obiettivo"}, "sort_order": 1}, {"value": "Adapter", "label": "Adapter", "labels": {"en": "Adapter", "cs": "Adaptér", "it": "Adattatore"}, "sort_order": 2}, {"value": "Light", "label": "Light", "labels": {"en": "Light", "cs": "Světlo", "it": "Luce"}, "sort_order": 3}, {"value": "PC", "label": "PC", "labels": {"en": "PC", "cs": "PC", "it": "PC"}, "sort_order": 4}, {"value": "Router", "label": "Router", "labels": {"en": "Router", "cs": "Router", "it": "Router"}, "sort_order": 5}, {"value": "Power Supply", "label": "Power Supply", "labels": {"en": "Power Supply", "cs": "Napájecí zdroj", "it": "Alimentatore"}, "sort_order": 6}, {"value": "Monitor", "label": "Monitor", "labels": {"en": "Monitor", "cs": "Monitor", "it": "Monitor"}, "sort_order": 7}, {"value": "Keyboard", "label": "Keyboard", "labels": {"en": "Keyboard", "cs": "Klávesnice", "it": "Tastiera"}, "sort_order": 8}, {"value": "Mice", "label": "Mice", "labels": {"en": "Mice", "cs": "Myši", "it": "Mouse"}, "sort_order": 9}, {"value": "Cable", "label": "Cable", "labels": {"en": "Cable", "cs": "Kabel", "it": "Cavo"}, "sort_order": 10}, {"value": "Module/Block", "label": "Module/Block", "labels": {"en": "Module/Block", "cs": "Modul/Blok", "it": "Modulo/Blocco"}, "sort_order": 11}]

  function statusByValue(value) {
    for (var i = 0; i < STATUSES.length; i += 1) {
      if (STATUSES[i].value === value) return STATUSES[i]
    }
    return null
  }

  function typeByValue(value) {
    for (var i = 0; i < TYPES.length; i += 1) {
      if (TYPES[i].value === value) return TYPES[i]
    }
    return null
  }

  function iso(daysFromNow) {
    var d = new Date()
    d.setDate(d.getDate() + daysFromNow)
    return d.toISOString().slice(0, 10)
  }

  function stamp(offsetHours) {
    var d = new Date()
    d.setHours(d.getHours() - offsetHours)
    return d.toISOString()
  }

  // Synthetic equipment; option values match equipment-log/main.go defaults.
  var SEED = [["PV-001", "Basler ace acA1920", "BAS-001", "Camera", "Feasibility", 0, 0], ["PV-002", "Fujinon HF12.5SA-1", "FUJ-002", "Lens", "Devel", 0, 0], ["PV-003", "CCS LDR2 ring light", "CCS-003", "Light", "Feasibility", 0, 0], ["PV-004", "C-mount adapter", "CMT-004", "Adapter", "Sales", 0, 0], ["PV-005", "Advantech MIC-770", "ADV-005", "PC", "Customer", 18, 3], ["PV-006", "Moxa EDS-205", "MOX-006", "Router", "Devel", 0, 0], ["PV-007", "Mean Well 24V", "MW-007", "Power Supply", "Feasibility", 0, 0], ["PV-008", "Dell UltraSharp U2723QE", "DEL-008", "Monitor", "Customer", 20, 5], ["PV-009", "Logitech MX Keys", "LOG-009", "Keyboard", "Sales", 0, 0], ["PV-010", "Logitech MX Master", "LOG-010", "Mice", "Sales", 0, 0], ["PV-011", "GigE CAT6 5m", "CAB-011", "Cable", "Loaned", 12, 20], ["PV-012", "I/O module", "MOD-012", "Module/Block", "Devel", 0, 0], ["PV-013", "Basler ace 2 a2A2448", "BAS-013", "Camera", "Loaned", 10, 45], ["PV-014", "Computar M0814-MP2", "COM-014", "Lens", "Customer", 7, 15], ["PV-015", "Smart Vision backlight", "SV-015", "Light", "Feasibility", 0, 0], ["PV-016", "Datalogic P3x", "DL-016", "Camera", "Customer", 8, 0], ["PV-017", "USB-C power adapter", "USBC-017", "Adapter", "Feasibility", 0, 0], ["PV-018", "Industrial trigger cable", "CAB-018", "Cable", "Loaned", 10, 4]]

  var items = SEED.map(function (row, index) {
    var type = typeByValue(row[3])
    var status = statusByValue(row[4])
    return {
      id: index + 1,
      asset_tag: row[0],
      name: row[1],
      sn: row[2],
      type: row[3],
      type_label: type ? type.label : row[3],
      status: row[4],
      status_label: status ? status.label : row[4],
      status_color: status ? status.color : '#cbd5e1',
      sent_date: row[5] > 0 ? iso(-row[5]) : null,
      return_date: row[5] > 0 ? iso(index % 3 === 0 ? -3 : 7) : null,
      note: index % 4 === 0 ? 'Demo equipment record — inspect before the next handover.' : '',
      version: 1,
      archived: false,
      label_printed_at: index % 3 === 0 ? stamp(72) : null,
      created_at: stamp(24 * 200),
      updated_at: stamp(index * 5 + 1),
      created_by: 'admin',
      updated_by: 'admin',
    }
  })

  var USERS = [
    {
      id: 1,
      username: 'admin',
      display_name: 'Admin',
      email: 'david@example.com',
      role: 'admin',
      must_change_password: false,
      active: true,
      last_login_at: stamp(2),
      created_at: stamp(24 * 365),
      updated_at: stamp(2),
    },
    {
      id: 2,
      username: 'editor',
      display_name: 'Editor',
      email: 'marek@example.com',
      role: 'editor',
      must_change_password: false,
      active: true,
      last_login_at: stamp(30),
      created_at: stamp(24 * 300),
      updated_at: stamp(30),
    },
  ]

  var SETTINGS = {
    discord: { webhook_set: true, webhook_preview: 'https://discord.com/api/webhooks/…/••••', enabled: true },
    scheduler: { check_hour: 11, days_ahead: 0 },
    locale: { default: 'en' },
    general: { organization_name: 'Sklad · Equipment workspace' },
  }

  var DELIVERIES = [
    { id: 5, channel_id: 1, trigger: 'overdue_daily', status: 'sent', item_count: 3, error: null, created_at: stamp(5), sent_at: stamp(5) },
    { id: 4, channel_id: 1, trigger: 'manual', status: 'sent', item_count: 1, error: null, created_at: stamp(28), sent_at: stamp(28) },
    { id: 3, channel_id: 1, trigger: 'overdue_daily', status: 'failed', item_count: 0, error: 'Discord returned 429 (rate limited)', created_at: stamp(53), sent_at: null },
    { id: 2, channel_id: 1, trigger: 'test', status: 'sent', item_count: 0, error: null, created_at: stamp(76), sent_at: stamp(76) },
    { id: 1, channel_id: 1, trigger: 'overdue_daily', status: 'sent', item_count: 2, error: null, created_at: stamp(101), sent_at: stamp(101) },
  ]

  var PEOPLE = [{id:1,name:'Customer',active:true,is_customer:true,version:1},{id:2,name:'Demo colleague',active:true,is_customer:false,version:1}]
  var loggedIn = false
  var nextItemId = items.length + 1
  var nextUserId = USERS.length + 1
  var nextDeliveryId = 6
  var currentUserId = 0
  var locale = 'en'
  var storageKey = 'sklad-original-preview-v1'
  try {
    var saved = JSON.parse(localStorage.getItem(storageKey) || 'null')
    if (saved && saved.items && saved.types && saved.statuses) {
      items = saved.items; TYPES = saved.types; STATUSES = saved.statuses
      PEOPLE = saved.personnel || PEOPLE; USERS = saved.users || USERS; SETTINGS = saved.settings || SETTINGS
      DELIVERIES = saved.deliveries || DELIVERIES
      nextItemId = Math.max(0, ...items.map(function (i) { return i.id })) + 1
      nextUserId = Math.max(0, ...USERS.map(function (u) { return u.id })) + 1
      nextDeliveryId = Math.max(0, ...DELIVERIES.map(function (d) { return d.id })) + 1
    }
    loggedIn = !!sessionStorage.getItem('sklad-public-session-v2') && sessionStorage.getItem('sklad-public-session-v2') !== 'logged-out'
    currentUserId = Number(sessionStorage.getItem('sklad-public-session-v2')) || 0
  } catch (_) { /* Private browsing may disable storage. */ }
  USERS = USERS.filter(function (u) { return u.username === 'admin' && u.role === 'admin' })
  USERS.forEach(function (u) { u.display_name = u.role === 'admin' ? 'Admin' : 'Editor' })
  loggedIn = loggedIn && currentUserId === 1
  items.forEach(function(i){if(i.personnel_id === undefined){i.personnel_id = null;i.personnel_name = null}})
  function persist() {
    try { localStorage.setItem(storageKey, JSON.stringify({items: items, personnel: PEOPLE, types: TYPES, statuses: STATUSES, users: USERS, settings: SETTINGS, deliveries: DELIVERIES})) } catch (_) { /* Keep in-memory editing available. */ }
    try { sessionStorage.setItem('sklad-public-session-v2', loggedIn ? String(currentUserId) : 'logged-out') } catch (_) {}
  }
  function currentUser() { return (loggedIn && USERS.find(function (u) { return u.id === currentUserId })) || { role: 'editor' } }
  function validateRental(item,patch,admin){
    var allowed=['version','status','sent_date','return_date','personnel_id','note','clear_sent_date','clear_return_date'].concat(admin?['asset_tag','name','sn','type']:[])
    if(Object.keys(patch).some(function(k){return allowed.indexOf(k)<0}))return 'Only rental fields may be changed'
    if(item.archived && !admin)return 'Archived equipment cannot be rented'
    if(Object.keys(patch).some(function(k){return patch[k]===null}))return 'Rental values cannot be null; use an empty date or zero renter to clear'
    var target=Object.assign({},item,patch)
    if(patch.clear_sent_date)target.sent_date=null
    if(patch.clear_return_date)target.return_date=null
    if(!statusByValue(target.status))return 'Select a valid status'
    if(target.status==='Feasibility'){
      if(item.status!=='Feasibility'&&!admin)return 'Only Admin can change an item back to Feasibility'
      if(['sent_date','return_date','personnel_id','note'].some(function(k){return (target[k]||'')!==(item[k]||'')}))return 'Rental fields are locked in Feasibility'
    }else{
      var person=PEOPLE.find(function(p){return p.id===target.personnel_id})
      if(!person)return 'Personnel renting the device is required'
      if(!person.active&&(item.personnel_id!==person.id||item.status==='Feasibility'))return 'Select an active person'
      if(person.is_customer&&!String(target.note||'').trim())return "Note must contain the customer's name"
    }
    return ''
  }
  function refreshLabels() {
    STATUSES.forEach(function(s){var fixed={feasibility:'Feasibility',devel:'Devel',sales:'Sales'}[String(s.value).toLowerCase()];if(fixed){s.labels={en:fixed,cs:fixed,it:fixed};s.label=fixed}})
    TYPES.concat(STATUSES).forEach(function (o) { o.label = (o.labels && (o.labels[locale] || o.labels.en)) || o.label || o.value })
    items.forEach(function (i) {
      var t = typeByValue(i.type), st = statusByValue(i.status)
      var person = PEOPLE.find(function(p){return p.id===i.personnel_id});i.personnel_name=person?person.name:null
      i.type_label = t ? t.label : i.type
      i.status_label = st ? st.label : i.status
      i.status_color = st ? st.color : '#cbd5e1'
    })
  }
  refreshLabels()

  // ------------------------------------------------------------------ helpers --

  function json(body, status) {
    return new Response(JSON.stringify(body), {
      status: status || 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  function error(status, code, message) {
    return json({ error: { code: code, message: message } }, status)
  }

  function readBody(init) {
    if (!init || typeof init.body !== 'string' || !init.body) return {}
    try {
      return JSON.parse(init.body)
    } catch (e) {
      return {}
    }
  }

  function num(value, fallback) {
    var parsed = Number(value)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
  }

  function today() {
    return new Date().toISOString().slice(0, 10)
  }

  function isOverdue(item) {
    if (String(item.status || '').toLowerCase() === 'feasibility') return false
    return !item.archived && !!item.return_date && item.return_date <= today()
  }

  function listItems(url) {
    var q = (url.searchParams.get('q') || '').trim().toLowerCase()
    var type = url.searchParams.get('type') || ''
    var status = url.searchParams.get('status') || ''
    var archived = url.searchParams.get('archived') === 'true'
    var overdue = url.searchParams.get('overdue') === 'true'
    var sort = url.searchParams.get('sort') || 'asset_tag'
    var order = url.searchParams.get('order') === 'desc' ? -1 : 1
    var page = num(url.searchParams.get('page'), 1)
    var perPage = num(url.searchParams.get('per_page'), 20)

    var rows = items.filter(function (item) {
      if (!loggedIn && item.archived) return false
      if (item.archived !== archived) return false
      if (type && item.type !== type) return false
      if (status && item.status !== status) return false
      if (overdue && !isOverdue(item)) return false
      if (q) {
        var haystack = (item.asset_tag + ' ' + item.name + ' ' + item.sn + ' ' + (item.type_label || '') + ' ' + (item.status_label || '')).toLowerCase()
        if (haystack.indexOf(q) === -1) return false
      }
      return true
    })

    rows.sort(function (a, b) {
      var av = a[sort] == null ? '' : a[sort]
      var bv = b[sort] == null ? '' : b[sort]
      if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * order
      return String(av).localeCompare(String(bv), undefined, { numeric: true }) * order
    })

    var total = rows.length
    var totalPages = Math.max(1, Math.ceil(total / perPage))
    var start = (page - 1) * perPage
    return {
      data: rows.slice(start, start + perPage),
      meta: { page: page, per_page: perPage, total: total, total_pages: totalPages },
    }
  }

  // ------------------------------------------------------------------ routing --

  function handle(path, method, url, init) {
    // --- system -------------------------------------------------------------
    if (method === 'GET' && path === '/health') {
      return json({ status: 'ok', version: '1.0.0-preview', time: new Date().toISOString() })
    }
    if (method === 'GET' && path === '/version') {
      return json({ version: '1.0.0-sites-preview', commit: 'original-react-ui', build_time: stamp(24 * 5) })
    }

    // --- auth ---------------------------------------------------------------
    if (method === 'GET' && path === '/auth/me') {
      if (!loggedIn) return error(401, 'unauthorized', 'Unauthorized')
      return json({ data: currentUser() })
    }
    if (method === 'POST' && path === '/auth/login') {
      var loginBody = readBody(init)
      var account = USERS.find(function (u) { return u.username === loginBody.username && u.active })
      if (!account || !String(loginBody.password || "").trim()) return error(401, 'unauthorized', 'Use the Admin preview account.')
      currentUserId = account.id
      loggedIn = true
      return json({ data: { user: account, csrf_token: 'preview-csrf-token' } })
    }
    if (method === 'POST' && path === '/auth/logout') {
      loggedIn = false
      return new Response(null, { status: 204 })
    }
    if (method === 'POST' && path === '/auth/change-password') {
      currentUser().must_change_password = false
      return json({ data: { ok: true } })
    }

    var publicRead = (method === 'GET' && (/^\/items(?:\/\d+)?$/.test(path) || path === '/options' || path === '/personnel')) || (method === 'POST' && path === '/items/resolve')
    if (!loggedIn && path === '/items' && url.searchParams.get('archived') === 'true') return error(403, 'forbidden', 'Archived equipment is staff-only')
    var rentalWrite = (method === 'PATCH' && /^\/items\/\d+$/.test(path)) || (method === 'POST' && path === '/items/bulk')
    if (!loggedIn && !publicRead && !rentalWrite) return error(401, 'unauthorized', 'Unauthorized')
    if ((/^\/(users|options|settings|notifications)/.test(path) && !(path === '/options' && method === 'GET')) && currentUser().role !== 'admin') return error(403, 'forbidden', 'Admin role required')


    if (path === '/personnel' && method === 'GET') return json({data:PEOPLE})
    if (/^\/personnel(?:\/\d+)?$/.test(path)) {
      if(currentUser().role !== 'admin')return error(403,'forbidden','Admin required')
      var personBody=readBody(init), person=PEOPLE.find(function(p){return p.id===Number(path.split('/')[2])})
      if(method==='POST'){
        if(!String(personBody.name||'').trim()||String(personBody.name).trim().toLowerCase()==='customer')return error(400,'validation_failed','Personnel name required; Customer is reserved')
        var newPerson={id:Math.max.apply(null,PEOPLE.map(function(p){return p.id}))+1,name:personBody.name.trim(),active:true,is_customer:false,version:1};PEOPLE.push(newPerson);return json({data:newPerson},201)
      }
      if(method==='PATCH' && person){
        if(person.is_customer)return error(400,'validation_failed','Customer is permanent')
        if(personBody.version!==person.version)return error(409,'version_conflict','Refresh personnel')
        if(personBody.name!==undefined && (!personBody.name.trim()||personBody.name.trim().toLowerCase()==='customer'))return error(400,'validation_failed','Valid personnel name required')
        if(personBody.name!==undefined)person.name=personBody.name.trim()
        if(personBody.active!==undefined)person.active=personBody.active
        person.version++;refreshLabels();return json({data:person})
      }
      return error(404,'not_found','Person not found')
    }
    // --- users --------------------------------------------------------------
    if (path === '/users' && method === 'GET') return json({ data: USERS })
    if (path === '/users' && method === 'POST') return error(403,'forbidden','Editor is public; additional accounts cannot be created')
    if (false) {
      var input = readBody(init)
      if (input.username !== 'editor' || input.role !== 'editor') return error(400, 'validation_failed', 'Only the Editor account can be created.')
      if (USERS.some(function (u) { return u.username === 'editor' })) return error(409, 'conflict', 'Editor already exists.')
      var created = {
        id: nextUserId++,
        username: input.username || 'user',
        display_name: input.display_name || input.username || 'New User',
        email: input.email || '',
        role: input.role || 'viewer',
        must_change_password: true,
        active: true,
        last_login_at: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      }
      USERS.push(created)
      return json({ data: created })
    }
    if (method === 'PATCH' && path.indexOf('/users/') === 0) {
      var userId = Number(path.slice('/users/'.length))
      var patchUser = readBody(init)
      delete patchUser.password // Never retain credentials in this browser preview.
      for (var u = 0; u < USERS.length; u += 1) {
        if (USERS[u].id === userId) {
          if (patchUser.role && patchUser.role !== USERS[u].role) return error(400, 'validation_failed', 'Account roles are fixed.')
          Object.assign(USERS[u], patchUser, { updated_at: new Date().toISOString() })
          return json({ data: USERS[u] })
        }
      }
      return error(404, 'not_found', 'User not found')
    }
    if (method === 'DELETE' && path.indexOf('/users/') === 0) {
      var delUserId = Number(path.slice('/users/'.length))
      for (var d = 0; d < USERS.length; d += 1) {
        if (USERS[d].id === delUserId) USERS[d].active = false
      }
      return json({ data: { ok: true } })
    }

    // --- options ------------------------------------------------------------
    if (method === 'GET' && path === '/options') { locale = url.searchParams.get('lang') || locale; refreshLabels(); return json({ data: { types: TYPES, statuses: STATUSES } }) }
    if (method === 'PUT' && path === '/options') {
      var optionsBody = readBody(init)
      if(!optionsBody.statuses.some(function(s){return s.value==='Feasibility'}))return error(400,'validation_failed','Feasibility must remain available')
      var removedType = TYPES.find(function (o) { return !optionsBody.types.some(function (n) { return n.value === o.value }) && items.some(function (i) { return i.type === o.value }) })
      var removedStatus = STATUSES.find(function (o) { return !optionsBody.statuses.some(function (n) { return n.value === o.value }) && items.some(function (i) { return i.status === o.value }) })
      if (removedType || removedStatus) return error(409, 'conflict', 'Cannot remove an option used by equipment.')
      if (Array.isArray(optionsBody.types)) TYPES = optionsBody.types
      if (Array.isArray(optionsBody.statuses)) STATUSES = optionsBody.statuses
      refreshLabels()
      return json({ data: { types: TYPES, statuses: STATUSES } })
    }

    // --- settings -----------------------------------------------------------
    if (method === 'GET' && path === '/settings') return json({ data: SETTINGS })
    if (method === 'PUT' && path === '/settings') {
      var settingsBody = readBody(init)
      if (settingsBody.discord) {
        Object.assign(SETTINGS.discord, {
          enabled: settingsBody.discord.enabled != null ? settingsBody.discord.enabled : SETTINGS.discord.enabled,
        })
        if (settingsBody.discord.webhook_url) {
          SETTINGS.discord.webhook_set = true
          SETTINGS.discord.webhook_preview = 'https://discord.com/api/webhooks/…/••••'
        }
        if (settingsBody.discord.clear_webhook) {
          SETTINGS.discord.webhook_set = false
          SETTINGS.discord.webhook_preview = ''
        }
      }
      if (settingsBody.scheduler) Object.assign(SETTINGS.scheduler, settingsBody.scheduler)
      if (settingsBody.locale) Object.assign(SETTINGS.locale, settingsBody.locale)
      if (settingsBody.general) Object.assign(SETTINGS.general, settingsBody.general)
      return json({ data: SETTINGS })
    }

    // --- notifications ------------------------------------------------------
    if (method === 'GET' && path === '/notifications') {
      return json({ data: DELIVERIES, meta: { page: 1, per_page: 20, total: DELIVERIES.length, total_pages: 1 } })
    }
    if (method === 'POST' && path === '/notifications/test') {
      DELIVERIES.unshift({ id: nextDeliveryId++, channel_id: 1, trigger: 'test', status: 'sent', item_count: 0, error: null, created_at: new Date().toISOString(), sent_at: new Date().toISOString() })
      return json({ data: { ok: true } })
    }
    if (method === 'POST' && path === '/notifications/run-now') {
      var due = items.filter(isOverdue).length
      DELIVERIES.unshift({ id: nextDeliveryId++, channel_id: 1, trigger: 'manual', status: 'sent', item_count: due, error: null, created_at: new Date().toISOString(), sent_at: new Date().toISOString() })
      return json({ data: { due_count: due, sent: true, delivery_id: nextDeliveryId - 1 } })
    }

    // --- export -------------------------------------------------------------
    if (method === 'GET' && path === '/export') {
      var format = url.searchParams.get('format') || 'json'
      if (format === 'csv') {
        var header = 'asset_tag,name,sn,type,status,status_color,sent_date,return_date,note\n'
        var body = items.map(function (item) {
          return [item.asset_tag, item.name, item.sn, item.type, item.status, item.status_color, item.sent_date || '', item.return_date || '', item.note.replace(/,/g, ';')].join(',')
        }).join('\n')
        return new Response(header + body, { status: 200, headers: { 'Content-Type': 'text/csv' } })
      }
      return json({ data: items })
    }

    // --- items --------------------------------------------------------------
    if (method === 'GET' && path === '/items') return json(listItems(url))

    if (method === 'GET' && path.indexOf('/items/') === 0) {
      var getId = Number(path.slice('/items/'.length))
      var found = items.filter(function (item) { return item.id === getId })[0]
      return found && (loggedIn || !found.archived) ? json({ data: found }) : error(404, 'not_found', 'Item not found')
    }

    if (method === 'POST' && path === '/items') {
      var createBody = readBody(init)
      createBody.status=createBody.status || "Feasibility"
      var createProblem=validateRental({status:"Feasibility",note:"",sent_date:null,return_date:null,personnel_id:null},createBody,true)
      if(createProblem)return error(400,"validation_failed",createProblem)
      if (items.some(function (i) { return i.asset_tag.toLowerCase() === String(createBody.asset_tag || '').toLowerCase() })) return error(409, 'conflict', 'Duplicate asset tag')
      var createType = typeByValue(createBody.type)
      var createStatus = statusByValue(createBody.status)
      var newItem = {
        id: nextItemId++,
        asset_tag: createBody.asset_tag || 'PV-' + String(nextItemId).padStart(3, '0'),
        name: createBody.name || '',
        sn: createBody.sn || '',
        type: createBody.type || null,
        type_label: createType ? createType.label : createBody.type || null,
        status: createBody.status || null,
        status_label: createStatus ? createStatus.label : createBody.status || null,
        status_color: createStatus ? createStatus.color : '#cbd5e1',
        sent_date: createBody.sent_date || null,
        return_date: createBody.return_date || null,
        note: createBody.note || '',
        personnel_id: createBody.personnel_id || null,
        version: 1,
        archived: false,
        label_printed_at: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        created_by: 'admin',
        updated_by: 'admin',
      }
      items.unshift(newItem)
      return json({ data: newItem })
    }

    if ((method === 'PATCH' || method === 'PUT') && path.indexOf('/items/') === 0) {
      var patchId = Number(path.slice('/items/'.length))
      var patchBody = readBody(init)
      for (var i = 0; i < items.length; i += 1) {
        if (items[i].id === patchId) {
          if (patchBody.version !== items[i].version) return error(409, 'version_conflict', 'Refresh the item and try again')
          var problem=validateRental(items[i],patchBody,currentUser().role==='admin')
          if(problem)return error(400,'validation_failed',items[i].asset_tag+': '+problem)
          delete patchBody.version
          Object.assign(items[i], patchBody)
          items[i].personnel_id = items[i].personnel_id || null
          items[i].updated_by = loggedIn ? currentUserId : null
          if (patchBody.type !== undefined) {
            var pt = typeByValue(patchBody.type)
            items[i].type_label = pt ? pt.label : patchBody.type
          }
          if (patchBody.status !== undefined) {
            var ps = statusByValue(patchBody.status)
            items[i].status_label = ps ? ps.label : patchBody.status
            items[i].status_color = ps ? ps.color : '#cbd5e1'
          }
          refreshLabels()
          items[i].version += 1
          items[i].updated_at = new Date().toISOString()
          return json({ data: items[i] })
        }
      }
      return error(404, 'not_found', 'Item not found')
    }

    if (method === 'DELETE' && path.indexOf('/items/') === 0) {
      var archiveId = Number(path.slice('/items/'.length))
      for (var a = 0; a < items.length; a += 1) {
        if (items[a].id === archiveId) {
          items[a].archived = true
          items[a].version += 1
        }
      }
      return json({ data: { ok: true } })
    }

    if (method === 'POST' && path === '/items/resolve') {
      var resolveBody = readBody(init)
      var codes = Array.isArray(resolveBody.codes) ? resolveBody.codes : []
      var matched = []
      var unknown = []
      codes.forEach(function (raw) {
        var code = String(raw).toUpperCase()
        var hit = items.filter(function (item) { return (loggedIn || !item.archived) && item.asset_tag.toUpperCase() === code })[0]
        if (hit) matched.push(hit)
        else unknown.push(code)
      })
      return json({ data: { matched: matched, unknown: unknown } })
    }

    if (method === 'POST' && path === '/items/bulk') {
      var bulkBody = readBody(init)
      var refs = Array.isArray(bulkBody.items) ? bulkBody.items : []
      var patch = bulkBody.patch || {}
      if (refs.some(function (ref) { var i = items.find(function (i) { return i.asset_tag === ref.asset_tag }); return !i || i.version !== ref.version })) return error(409, 'version_conflict', 'Refresh the buffer and try again')
      if (!refs.length || new Set(refs.map(function(r){return r.asset_tag})).size!==refs.length)return error(400,'validation_failed','Select unique items')
      var problems=refs.map(function(ref){var i=items.find(function(i){return i.asset_tag===ref.asset_tag});var problem=validateRental(i,patch,currentUser().role==='admin');return problem?i.asset_tag+': '+problem:''}).filter(Boolean)
      if(problems.length)return error(400,'validation_failed',problems.join('\n'))
      var updatedItems = refs.map(function (ref) {
        var target = items.filter(function (item) { return item.asset_tag === ref.asset_tag })[0]
        if (!target) return null
        if (patch.status !== undefined) {
          target.status = patch.status
          var bs = statusByValue(patch.status)
          target.status_label = bs ? bs.label : patch.status
          target.status_color = bs ? bs.color : '#cbd5e1'
        }
        if (patch.personnel_id !== undefined) target.personnel_id=patch.personnel_id || null
        if (patch.note !== undefined) target.note = patch.note
        if (patch.sent_date !== undefined) target.sent_date = patch.sent_date
        if (patch.return_date !== undefined) target.return_date = patch.return_date
        if (patch.clear_sent_date) target.sent_date = null
        if (patch.clear_return_date) target.return_date = null
        target.version += 1
        target.updated_at = new Date().toISOString()
        return target
      }).filter(Boolean)
      refreshLabels()
      return json({ data: { updated: updatedItems.length, updated_items: updatedItems } })
    }

    return error(404, 'not_found', 'Unmocked ' + method + ' ' + path)
  }

  // --------------------------------------------------------------- interception --

  window.fetch = function (input, init) {
    var raw
    try {
      raw = typeof input === 'string' ? input : input && input.url
    } catch (e) {
      raw = null
    }
    if (!raw) return origFetch(input, init)

    var url
    try {
      url = new URL(raw, window.location.origin)
    } catch (e) {
      return origFetch(input, init)
    }

    if (url.pathname !== API && url.pathname.indexOf(API + '/') !== 0) {
      return origFetch(input, init)
    }

    var method = ((init && init.method) || (input && input.method) || 'GET').toUpperCase()
    var path = url.pathname.slice(API.length)
    try {
      var response = handle(path, method, url, init)
      if (method !== 'GET' && response.ok) persist()
      return Promise.resolve(response)
    } catch (e) {
      return Promise.resolve(error(500, 'preview_error', String(e && e.message ? e.message : e)))
    }
  }

  window.__SKLAD_PREVIEW__ = true

})()
