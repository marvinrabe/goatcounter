import Chart from 'chart.js/auto'

// Set up the entire dashboard page.
export function init_dashboard() {
	report_ajax_errors()
	bind_site_selector()
	bind_tracking_code()
	bind_header()
	bind_widgets()
	bind_metric_chart()
	init_widgets()
}

// Requests to load more widget data fail silently; tell about the others.
var report_ajax_errors = function() {
	document.addEventListener('ajaxerror', function(e) {
		let {url, error} = e.detail
		if (url === '/load-widget')
			return
		let msg = `Could not load ${url}: ${error}`
		console.error(msg)
		alert(msg)
	})
}

var bind_site_selector = function() {
	on('#site-selector', 'change', function() {
		// Keep the period and grouping, but not the paths of the other site.
		let url = new URL(location.href)
		url.searchParams.set('site', this.value)
		url.searchParams.delete('filter')
		url.searchParams.delete('showrefs')
		location.href = url.toString()
	})
}

var bind_tracking_code = function() {
	const dropdown = $1('#tracking-code')
	if (!dropdown) return
	const snippet = $1('#tracking-snippet'), status = $1('#copy-code-status')
	snippet.addEventListener('click', () => snippet.select())
	$1('#copy-tracking-code').addEventListener('click', async () => {
		status.textContent = ''
		try {
			await navigator.clipboard.writeText(snippet.value)
			status.textContent = 'Copied!'
		} catch (_) {
			// Clipboard access may be unavailable on a plain HTTP installation.
			snippet.focus()
			snippet.select()
			let copied = false
			try { copied = document.execCommand('copy') } catch (_) { }
			status.textContent = copied ? 'Copied!' : 'Code selected. Press ⌘C or Ctrl+C to copy.'
		}
	})
	document.addEventListener('click', (e) => {
		if (!dropdown.contains(e.target)) dropdown.open = false
	})
	document.addEventListener('keydown', (e) => {
		if (e.key === 'Escape' && dropdown.open) {
			dropdown.open = false
			$1('summary', dropdown).focus()
		}
	})
}

// Set up the widget contents; this runs again after they're reloaded.
var init_widgets = function() {
	$$('.totals').forEach((totals) => {
		let saved = $$('.metric[data-metric]', totals).find((b) => b.dataset.metric === dashboard_view().metric)
		if (saved)
			select_metric(totals, saved)
	})
	$$('[data-card-tabs]').forEach((card) => {
		let saved = $$('.card-tab[data-panel]', card).find((t) => t.dataset.panel === dashboard_view()[card.dataset.card])
		if (saved)
			select_card_tab(card, saved)
	})
	redraw_metric_chart()
	highlight_filter()
}

// The selected metric and tabs are remembered for the browser tab.
var dashboard_view_key = 'goatcounter-dashboard-view'
var dashboard_view = function() {
	try {
		return JSON.parse(sessionStorage.getItem(dashboard_view_key)) || {}
	} catch (_) {
		return {}
	}
}
var save_dashboard_view = function(name, value) {
	try {
		let view = dashboard_view()
		view[name] = value
		sessionStorage.setItem(dashboard_view_key, JSON.stringify(view))
	} catch (_) { }
}

var select_metric = function(totals, button) {
	$$('.metric', totals).forEach((m) => m.setAttribute('aria-pressed', m === button ? 'true' : 'false'))
	let chart = $1('.chart[data-series]', totals)
	if (chart)
		chart.dataset.metric = button.dataset.metric
}

var select_card_tab = function(card, tab) {
	$$('.card-tab[data-panel]', card).forEach((t) => t.setAttribute('aria-selected', t === tab ? 'true' : 'false'))
	$$('.card-panel[data-panel]', card).forEach((p) => { p.hidden = p.dataset.panel !== tab.dataset.panel })
}

// Get the number of visits in the current dashboard selection.
var get_total = () => $1('#dash-total')?.textContent || ''

// Add the current dashboard selection to the data object.
var append_period = function(data) {
	data = data || {}
	data['site']         = $1('#site-selector').value
	data['period-start'] = $1('#period-start').value
	data['period-end']   = $1('#period-end').value
	data['filter']       = $1('#filter-paths').value
	data['group']        = $1('#hl-group').value
	return data
}

// Reload all widgets on the dashboard.
var reload_dashboard = function(done) {
	ajax('/', {
		data: append_period({reload: 't'}),
		success: function(data) {
			$1('#dash-timerange').innerHTML = data.timerange
			$1('#dash-total').textContent = data.total
			$$('.widget', parse_html(data.widgets)).forEach((elem) => {
				$1(`.widget[data-widget="${elem.dataset.widget}"]`)?.replaceWith(elem)
			})
			init_widgets()
			if (done)
				done()
		},
	})
}

// Set the start and end period and submit the form.
var set_period = function(start, end) {
	$1('#period-start').value = format_date_ymd(start)
	$1('#period-end').value   = format_date_ymd(end)
	$1('#dash-form').requestSubmit()
}

var bind_header = function() {
	on('#dash-move', 'click', 'button', function(e) {
		e.preventDefault()
		let start = get_date($1('#period-start').value),
			end   = get_date($1('#period-end').value),
			[unit, dir] = this.value.split('-'),
			n = dir === 'b' ? -1 : 1
		switch (unit) {
			case 'day':   start.setDate(start.getDate() + n);         end.setDate(end.getDate() + n);         break
			case 'week':  start.setDate(start.getDate() + 7 * n);     end.setDate(end.getDate() + 7 * n);     break
			case 'month': start.setMonth(start.getMonth() + n);       end.setMonth(end.getMonth() + n);       break
			case 'year':  start.setFullYear(start.getFullYear() + n); end.setFullYear(end.getFullYear() + n); break
		}
		// Moving a whole month should end at the end of the month.
		if (unit === 'month' && start.getDate() === 1)
			end = new Date(start.getFullYear(), start.getMonth() + 1, 0)
		set_period(start, end)
	})

	on('#dash-form', 'submit', function(e) {
		// The server calculates the dates for the "Last …" shortcuts, in
		// the dashboard's timezone.
		if (e.submitter?.name === 'period') {
			$$('#period-start, #period-end').forEach((i) => { i.disabled = true })
			return
		}
		if (get_date($1('#period-start').value) <= get_date($1('#period-end').value)) {
			// Send the clicked group button's value, not the current one.
			if (e.submitter?.name === 'group')
				$1('#hl-group').disabled = true
			return
		}

		e.preventDefault()
		let end = $1('#period-end')
		if (!end.classList.contains('red')) {
			end.classList.add('red', 'border-red-500')
			end.insertAdjacentHTML('afterend', ' <span class="text-xs text-red-600">end date is before start date</span>')
		}
	})

	// Date inputs emit change while the year is still being typed.
	on('#period-start, #period-end', 'blur', function() {
		if (this.value && this.value !== this.defaultValue)
			this.form.requestSubmit()
	})

	// Reload the dashboard when typing in the filter input, so the user
	// won't have to press enter.
	let input = $1('#filter-paths'), timer
	on(input, 'keydown', (e) => {
		if (e.key === 'Enter')
			e.preventDefault()
	})
	on(input, 'input', () => {
		clearTimeout(timer)
		timer = setTimeout(() => {
			push_query({filter: input.value, showrefs: null})
			let loading = document.createElement('span')
			loading.className = 'absolute right-3 top-2 animate-pulse text-slate-400'
			loading.textContent = '…'
			input.insertAdjacentElement('afterend', loading)
			reload_dashboard(() => loading.remove())
		}, 300)
	})
}

// The widgets are replaced when reloading, so all events are delegated
// from the document.
var bind_widgets = function() {
	on(document, 'click', '.totals .metric[data-metric]', function() {
		let totals = this.closest('.totals'),
			chart  = $1('.chart[data-series]', totals)
		if (!chart || chart.dataset.metric === this.dataset.metric)
			return
		select_metric(totals, this)
		save_dashboard_view('metric', this.dataset.metric)
		redraw_metric_chart()
	})

	on(document, 'click', '[data-card-tabs] .card-tab[data-panel]', function() {
		let card = this.closest('[data-card-tabs]')
		select_card_tab(card, this)
		save_dashboard_view(card.dataset.card, this.dataset.panel)
	})

	// "Show more" and "Show less" of the charts and the pages list. The
	// links are in the element with the widget name, and the key and total
	// for a detail.
	let data_rows = (rows) => $$(':scope > .hchart-row', rows)

	on(document, 'click', '.load-btns .load-less', function(e) {
		e.preventDefault()
		let btns = this.closest('.load-btns'),
			rows = $1('.rows', btns.parentElement)
		data_rows(rows).slice(+rows.dataset.pagesize).forEach((r) => {
			if (r.nextElementSibling?.classList.contains('detail'))
				r.nextElementSibling.remove()
			r.remove()
		})
		this.classList.add('hidden')
		$1('.load-more', btns).classList.remove('hidden')
	})

	on(document, 'click', '.load-btns .load-more', function(e) {
		e.preventDefault()
		let btn   = this,
			btns  = btn.closest('.load-btns'),
			less  = $1('.load-less', btns),
			chart = btns.parentElement,
			rows  = $1('.rows', chart)
		if (!rows.dataset.pagesize)
			rows.dataset.pagesize = data_rows(rows).length

		let done = paginate_button(btn, () => {
			ajax('/load-widget', {
				data: append_period({
					widget: chart.dataset.widget,
					key:    chart.dataset.key || '',
					total:  chart.dataset.total || get_total(),
					offset: data_rows(rows).length,
				}),
				success: function(data) {
					rows.insertAdjacentHTML('beforeend', data.html)
					highlight_filter()
					btn.classList.toggle('hidden', !data.more)
					less.classList.remove('hidden')
					less.classList.toggle('border-l', data.more)
					less.classList.toggle('pl-3', data.more)
					done()
				},
			})
		})
	})

	// Show the detail of a row, or the referrers of a path.
	on(document, 'click', '.hchart .load-detail', function(e) {
		e.preventDefault()

		let row    = this.closest('.hchart-row'),
			chart  = row.closest('.hchart'),
			widget = chart.dataset.widget,
			key    = row.dataset.key,
			isPage = row.hasAttribute('data-id'),
			total  = row.dataset.detailTotal || get_total()
		if (row.nextElementSibling?.classList.contains('detail')) {
			row.nextElementSibling.remove()
			row.classList.remove('target')
			if (isPage)
				push_query({showrefs: null})
			return
		}
		if (isPage)
			push_query({showrefs: key})

		let bar = $1('.bar-c', this)
		let done = paginate_button(bar, () => {
			ajax('/load-widget', {
				data: append_period({widget: widget, key: key, total: total}),
				success: function(data) {
					$$('.detail', chart).forEach((d) => d.remove())
					$$('.target', chart).forEach((d) => d.classList.remove('target'))
					row.insertAdjacentHTML('afterend', data.html)
					row.classList.add('target')
					done()
				},
			})
		})
	})
}

// Highlight the filter in the paths of the pages list.
var filter_kw = /(\b(?:at:start|at:end|is:event|is:pageview|in:path)|\B:not)\b/g
var highlight_filter = function() {
	let val = $1('#filter-paths').value,
		kw  = val.match(filter_kw) || [],
		s   = val.replace(filter_kw, '').trim()
	if (s === '' || kw.includes(':not'))
		return

	let re = new RegExp((kw.includes('at:start') ? '^' : '') + quote_re(s) + (kw.includes('at:end') ? '$' : ''), 'gi')
	$$('.rows.pages .rlink .cutoff').forEach((elem) => {
		if ($1('mark', elem))  // Don't apply twice after pagination.
			return
		let text = elem.textContent, parts = [], last = 0
		for (let m of text.matchAll(re)) {
			// The same yellow as the selected period.
			let mark = document.createElement('mark')
			mark.className = 'rounded-sm bg-yellow-200 font-semibold text-yellow-950 dark:bg-yellow-300/20 dark:text-yellow-200'
			mark.textContent = m[0]
			parts.push(text.slice(last, m.index), mark)
			last = m.index + m[0].length
		}
		if (parts.length > 0)
			elem.replaceChildren(...parts, text.slice(last))
	})
}

// Quote special regexp characters.
var quote_re = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// Set query parameters – leaving the others alone – and push to history;
// null removes a parameter.
var push_query = function(params) {
	let q = new URLSearchParams(location.search)
	for (let [k, v] of Object.entries(params)) {
		if (v === null)
			q.delete(k)
		else
			q.set(k, v)
	}
	let s = q.toString()
	history.pushState(null, '', s === '' ? location.pathname : `?${s}`)
}

var metric_chart = null

var redraw_metric_chart = function() {
	metric_chart?.destroy()
	metric_chart = null

	let c = $1('.totals .chart[data-series]'),
		canvas = c && $1('canvas', c)
	if (canvas)
		metric_chart = draw_metric_chart(c, canvas, JSON.parse(c.dataset.series))
}

// Redraw with the colours for the current light or dark theme.
var bind_metric_chart = function() {
	window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', redraw_metric_chart)
	new MutationObserver(function(mutations) {
		if (mutations.some((m) => m.type === 'attributes' && m.attributeName === 'class'))
			redraw_metric_chart()
	}).observe(document.documentElement, {attributes: true})
}

var draw_metric_chart = function(c, canvas, series) {
	let dark   = window.matchMedia('(prefers-color-scheme: dark)').matches,
		metric = c.dataset.metric,
		points = series.points,
		prev   = series.prev || [],
		line   = dark ? '#f0abfc' : '#c026d3',
		fill   = dark ? 'rgba(240, 171, 252, .12)' : 'rgba(192, 38, 211, .10)',
		grid   = dark ? '#404040' : '#e2e8f0',
		muted  = dark ? '#a3a3a3' : '#94a3b8',
		counts = ['visitors', 'visits', 'pageviews'].includes(metric)

	return new Chart(canvas.getContext('2d'), {
		type: 'line',
		data: {
			labels: points.map((point) => metric_point_label(point, series.group)),
			datasets: [{
				data: points.map((point) => point[metric]),
				borderColor: line,
				backgroundColor: fill,
				borderWidth: 1.25,
				fill: true,
				pointRadius: points.length <= 90 ? 2 : 0,
				pointHoverRadius: 3,
				pointHitRadius: 12,
				tension: 0,
			}, {
				// The previous period, for comparison.
				data: prev.map((point) => point[metric]),
				borderColor: muted,
				borderDash: [4, 4],
				borderWidth: 1,
				fill: false,
				pointRadius: 0,
				pointHoverRadius: 2,
				pointHitRadius: 12,
				tension: 0,
			}],
		},
		options: {
			responsive: true,
			maintainAspectRatio: false,
			animation: false,
			interaction: {mode: 'index', intersect: false},
			plugins: {
				legend: {display: false},
				tooltip: {
					displayColors: false,
					callbacks: {
						label: (item) => item.datasetIndex === 0
							? format_metric(metric, item.parsed.y)
							: `${format_metric(metric, item.parsed.y)} (${metric_point_label(prev[item.dataIndex], series.group)})`,
					},
				},
			},
			scales: {
				x: {
					border: {display: false},
					grid: {display: false},
					ticks: {autoSkip: true, maxTicksLimit: 8, maxRotation: 0, color: muted},
				},
				y: {
					beginAtZero: true,
					max: metric === 'bounce_rate' ? 100 : undefined,
					border: {display: false},
					grid: {color: grid},
					ticks: {
						color: muted,
						precision: counts ? 0 : undefined,
						callback: (value) => format_metric_tick(metric, value),
					},
				},
			},
		},
	})
}

var metric_point_label = function(point, group) {
	switch (group) {
		case 'year':
			return point.day.slice(0, 4)
		case 'hour':
			return `${format_date(point.day, true)} ${String(point.hour ?? 0).padStart(2, '0')}:00`
		case 'week': {
			let end = get_date(point.day)
			end.setDate(end.getDate() + 6)
			return `${format_date(point.day, true)} – ${format_date(end, true)}`
		}
		case 'month': {
			let date = get_date(point.day)
			return `${monthsShort[date.getMonth()]} ${date.getFullYear()}`
		}
	}
	return format_date(point.day, true)
}

var format_metric_tick = function(metric, value) {
	switch (metric) {
		case 'bounce_rate':     return `${value}%`
		case 'visit_duration':  return format_duration(value, false)
		case 'views_per_visit': return Number(value).toFixed(1)
	}
	return format_int(Math.round(value))
}

var format_metric = function(metric, value) {
	switch (metric) {
		case 'views_per_visit': return `${value.toFixed(2)} views per visit`
		case 'bounce_rate':     return `${value.toFixed(0)}% bounce rate`
		case 'visit_duration':  return `${format_duration(value, true)} visit duration`
	}
	return `${format_int(Math.round(value))} ${metric}`
}

var format_duration = function(value, includeSeconds) {
	let seconds = Math.round(value),
		hours   = Math.floor(seconds / 3600),
		minutes = Math.floor((seconds % 3600) / 60),
		parts   = []
	if (hours)
		parts.push(`${hours}h`)
	if (minutes)
		parts.push(`${minutes}m`)
	if (includeSeconds || parts.length === 0)
		parts.push(`${seconds % 60}s`)
	return parts.join(' ')
}

// Minimal DOM helpers, so that this doesn't need jQuery.

// Select elements; $$ always returns an array (so array methods work), $1 a
// single element or null. Both take an optional root to search within, which
// may be a document fragment.
var $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel)),
	$1 = (sel, root) => (root || document).querySelector(sel)

// Bind an event handler.
//
// root may be an element, an array of elements, or a selector matching the
// elements to bind on. sel is optional; if given the event is delegated to
// descendants matching it, and the handler is called with the matched element
// as "this" (like jQuery did).
//
// events is one or more space-separated event names.
var on = function(root, events, sel, fn) {
	if (typeof sel === 'function')
		[sel, fn] = [null, sel]

	let roots = typeof root === 'string' ? $$(root) : (Array.isArray(root) ? root : [root])
	roots.forEach((r) => {
		if (!r)
			return
		events.split(' ').filter((e) => e !== '').forEach((ev) => {
			if (!sel)
				return r.addEventListener(ev, (e) => fn.call(r, e))

			// mouseenter doesn't bubble, so it can't be delegated directly;
			// approximate it with mouseover, ignoring moves that stay within
			// the same matched element.
			let native = ev === 'mouseenter' ? 'mouseover' : ev
			r.addEventListener(native, function(e) {
				let t = e.target?.closest?.(sel)
				if (!t || !r.contains(t))
					return
				if (native !== ev && t.contains(e.relatedTarget))
					return
				fn.call(t, e)
			})
		})
	})
}

// Parse an HTML string, returning a document fragment. A <template> is used
// rather than innerHTML on a <div> so that table fragments (<tr>, <td>) parse
// correctly too.
var parse_html = function(html) {
	let t = document.createElement('template')
	t.innerHTML = (html || '').trim()
	return t.content
}

// Send a GET request, with opt.data as the query string. opt.success is
// called with the response, parsed as JSON if the server said it's JSON.
//
// Failures are reported as an "ajaxerror" event on document, which
// report_ajax_errors listens for. Errors thrown by opt.success are left alone,
// so they get reported as regular errors rather than request failures.
var ajax = function(url, opt) {
	opt = opt || {}
	let params = new URLSearchParams()
	Object.entries(opt.data || {}).forEach(([k, v]) => params.set(k, v === null || v === undefined ? '' : v))
	let q = params.toString()

	return fetch(q === '' ? url : `${url}?${q}`).
		then((r) => {
			if (!r.ok)
				throw new Error(`${r.status} ${r.statusText}`)
			return (r.headers.get('Content-Type') || '').startsWith('application/json') ? r.json() : r.text()
		}).
		then(
			(data) => { if (opt.success) opt.success(data) },
			(err)  => { document.dispatchEvent(new CustomEvent('ajaxerror', {detail: {url: url, error: err.message}})) })
}

// Prevent a button/link from working while an AJAX request is in progress;
// otherwise smashing a "show more" button will load the same data twice.
//
// This also adds a subtle loading indicator after the link/button.
var paginate_button = function(btn, f) {
	if (!btn) {
		f.call(btn)
		return () => {}
	}
	if (btn.dataset.working === '1')
		return

	btn.dataset.working = '1'
	btn.classList.add('loading', 'animate-pulse')
	f.call(btn)
	return () => {
		delete btn.dataset.working
		btn.classList.remove('loading', 'animate-pulse')
	}
}

// Format a number with a thousands separator (a thin space, matching the
// server-side formatting). https://stackoverflow.com/a/2901298/660921
var format_int = (n) => (n+'').replace(/\B(?=(\d{3})+(?!\d))/g, '\u202f')

var monthsShort = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// Format a date as "2 Aug 2026"; the year is left off if it's the current year
// and no_year_if_current is set.
var format_date = function(date, no_year_if_current) {
	if (typeof(date) === 'string')
		date = get_date(date)

	let s = `${date.getDate()} ${monthsShort[date.getMonth()]}`
	if (no_year_if_current && date.getFullYear() === (new Date()).getFullYear())
		return s
	return `${s} ${date.getFullYear()}`
}

// Format a date as year-month-day.
var format_date_ymd = function(date) {
	if (typeof(date) === 'string')
		return date
	var m = date.getMonth() + 1,
		d = date.getDate()
	return date.getFullYear() + '-' +
		(m >= 10 ? m : ('0' + m)) + '-' +
		(d >= 10 ? d : ('0' + d));
}

// Create Date() object from "year-month-day hour:min:sec" string. Any of the
// parts to the right may be missing: "2017-06" will create a date on June 1st.
var get_date = function(str) {
	let s = str.split(/[: -]/)
	return new Date(s[0],
		parseInt((s[1] || 1), 10) - 1,
		(s[2] || 1),
		(s[3] || 0), (s[4] || 0), (s[5] || 0))
}
