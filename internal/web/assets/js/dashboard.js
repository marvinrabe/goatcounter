import {
	$$,
	$1,
	ajax,
	format_date,
	format_date_ymd,
	format_int,
	get_date,
	monthsShort,
	on,
	paginate_button,
	parse_html,
} from './helper.js'
import Chart from 'chart.js/auto'

// Set up the entire dashboard page.
export var page_dashboard = function() {
	;[bind_header, bind_widgets, bind_metric_chart].forEach((f) => f.call())
	init_widgets()
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
					let detail = document.createElement('div')
					detail.className = 'hchart detail ml-3 border-l-2 border-slate-200 dark:border-neutral-700 pl-4'
					detail.dataset.widget = widget
					detail.dataset.key    = key
					detail.dataset.total  = total
					detail.innerHTML = data.html
					row.insertAdjacentElement('afterend', detail)
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
	$$('.pages-list .rows.pages .rlink .cutoff').forEach((elem) => {
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
