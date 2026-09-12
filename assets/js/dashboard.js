import {
	$$,
	$1,
	T,
	ajax,
	format_date,
	format_date_ymd,
	format_int,
	get_date,
	is_visible,
	monthsShort,
	on,
	paginate_button,
	parse_html,
	parse_rows,
	style,
} from './helper.js'
import Chart from 'chart.js/auto'

;(function() {
	'use strict';

	// Set up the entire dashboard page.
	var page_dashboard = function() {
		;[dashboard_widgets, hdr_select_period, hdr_datepicker, hdr_filter,
		].forEach((f) => f.call())
	}
	window.page_dashboard = page_dashboard  // Directly setting window loses the name attr 🤷

	// Set up all the dashboard widget contents (but not the header).
	var dashboard_widgets = function() {
		;[init_metric_switcher, init_card_tabs, init_metric_chart, paginate_pages, hchart_detail].forEach((f) => f.call())
	}

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
		$$('.metric', totals).forEach((candidate) => {
			let active = candidate === button
			candidate.classList.toggle('metric-active', active)
			candidate.setAttribute('aria-pressed', active ? 'true' : 'false')
		})
		let chart = $1('.chart[data-series]', totals)
		if (chart)
			chart.dataset.metric = button.dataset.metric
	}

	var init_metric_switcher = function() {
		$$('.totals').forEach(function(totals) {
			if (totals.dataset.metricBound === 't') return
			totals.dataset.metricBound = 't'

			let saved = dashboard_view().metric,
				savedButton = $$('.metric[data-metric]', totals).find((button) => button.dataset.metric === saved)
			if (savedButton)
				select_metric(totals, savedButton)

			totals.addEventListener('click', function(e) {
				let button = e.target.closest('.metric[data-metric]')
				if (!button) return
				let metric = button.dataset.metric,
					chart = $1('.chart[data-series]', totals)
				if (!chart || chart.dataset.metric === metric) return
				select_metric(totals, button)
				save_dashboard_view('metric', metric)
				redraw_metric_chart()
			})
		})
	}

	var select_card_tab = function(card, tab) {
		$$('.card-tab[data-panel]', card).forEach((candidate) => {
			let active = candidate === tab
			candidate.classList.toggle('active', active)
			candidate.setAttribute('aria-selected', active ? 'true' : 'false')
		})
		$$('.card-panel[data-panel]', card).forEach((panel) => {
			let active = panel.dataset.panel === tab.dataset.panel
			panel.hidden = !active
			panel.classList.toggle('active', active)
		})
	}

	var init_card_tabs = function() {
		$$('[data-card-tabs]').forEach(function(card) {
			if (card.dataset.tabsBound === 't') return
			card.dataset.tabsBound = 't'

			let saved = dashboard_view()[card.dataset.card],
				savedTab = $$('.card-tab[data-panel]', card).find((tab) => tab.dataset.panel === saved)
			if (savedTab)
				select_card_tab(card, savedTab)

			card.addEventListener('click', function(e) {
				let tab = e.target.closest('.card-tab[data-panel]')
				if (!tab) return
				select_card_tab(card, tab)
				save_dashboard_view(card.dataset.card, tab.dataset.panel)
			})
		})
	}

	// Direct element children of elem, which is what jQuery's ">div" did.
	var child_divs = (elem) => Array.from(elem.children).filter((c) => c.tagName === 'DIV')

	// Escape HTML special characters.
	var escape_html = (s) => s.replace(/[&<>"]/g, (c) => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;'})[c])

	// Keep the largest page count across paginated requests.
	var get_original_scale = function() { return parseInt($1('.count-list-pages')?.getAttribute('data-max'), 10) }

	// Get the total number of pageviews.
	var get_total = () => $1('.js-total-utc')?.textContent || ''

	// Reload a single widget.
	var reload_widget = function(wid, data, done) {
		data = data || {}
		data['widget'] = wid
		data['group']  = $1('#hl-group').value
		data['max']    = get_original_scale()
		data['total']  = get_total()

		ajax(BASE_PATH + '/load-widget', {
			data: append_period(data),
			success: function(data) {
				if (done)
					done()
				else {
					$$(`[data-widget="${wid}"]`).forEach((e) => { e.innerHTML = data.html })
					dashboard_widgets()
					highlight_filter()
				}
			},
		})
	}

	// Reload all widgets on the dashboard.
	let reload_dashboard = (done) => {
		ajax(`${BASE_PATH}/`, {
			data: append_period({
				group:     $1('#hl-group').value,
				max:       get_original_scale(),
				reload:    't',
			}),
			success: function(data) {
				$1('#dash-timerange').innerHTML = data.timerange
				$$('.widget-loaded', parse_html(data.widgets)).forEach((elem) => {
					$1(`[data-widget="${elem.dataset.widget}"]`)?.replaceWith(elem)
				})

				dashboard_widgets()
				highlight_filter()
				if (done)
					done()
			},
		})
	}

	// Append period-start and period-end values to the data object.
	var append_period = function(data) {
		data = data || {}
		data['site']         = $1('#site-selector').value
		data['period-start'] = $1('#period-start').value
		data['period-end']   = $1('#period-end').value
		data['filter']       = $1('#filter-paths').value
		return data
	}

	// Set the start and end period and submit the form.
	var set_period = function(start, end) {
		$1('#period-start').value = format_date_ymd(start)
		$1('#period-end').value   = format_date_ymd(end)
		$1('#dash-form').requestSubmit()
	}

	let filter_kw = /(\b(?:at:start|at:end|is:event|is:pageview|in:path)|\B:not)\b/g

	// Highlight a filter pattern in the path.
	let highlight_filter = () => {
		let val = $1('#filter-paths').value,
			s   = val.replace(filter_kw, '').trim()
		if (s === '')
			return

		let start   = '',
			end     = '',
			inPath  = false,
			kw      = val.match(filter_kw) || []
		for (let k of kw) {
			if (k === ':not')
				return
			else if (k === 'at:start')
				start = '^'
			else if (k === 'at:end')
				end = '$'
			else if (k === 'in:path')
				inPath = true
		}
		if (!inPath)
			inPath = true
		let where = []
		if (inPath)
			where.push('.rlink')

		$$('.pages-list .rows.pages').forEach((rows) => {
			$$(where.join(','), rows).forEach((elem) => {
				if ($1('b', elem))  // Don't apply twice after pagination
					return
				elem.innerHTML = elem.innerHTML.replace(new RegExp(start + quote_re(s) + end, 'gi'), '<b>$&</b>')
			})
		})
	}

	// Fill in start/end periods from buttons.
	var hdr_select_period = function() {
		on('#dash-select-group', 'click', 'button', function(e) {
			$1('#hl-period').removeAttribute('disabled')
		})

		on('#dash-select-period', 'click', 'button', function(e) {
			e.preventDefault()

			var start = new Date(), end = new Date()
			// Adjust the browser's "now" to the timezone from the user
			// settings, which is what the dashboard displays; near midnight
			// the two can be on different dates. The back/forward buttons
			// below need no adjustment, as they operate on the displayed
			// dates, which are already in that timezone.
			if (TZ_OFFSET) {
				var offset = (start.getTimezoneOffset() + TZ_OFFSET) / 60
				start.setHours(start.getHours() + offset)
				end.setHours(end.getHours() + offset)
			}
			switch (this.value) {
				case 'day':       /* Do nothing */ break
				case 'week':      start.setDate(start.getDate() - 7);   break;
				case 'month':     start.setMonth(start.getMonth() - 1); break;
				case 'quarter':   start.setMonth(start.getMonth() - 3); break;
				case 'half-year': start.setMonth(start.getMonth() - 6); break;
				case 'year':      start.setFullYear(start.getFullYear() - 1); break;
			}

			let p = $1('#hl-period')
			p.value = this.value
			p.removeAttribute('disabled')
			$1('#hl-group').removeAttribute('disabled')
			set_period(start, end)
		})

		on('#dash-move', 'click', 'button', function(e) {
			e.preventDefault()
			var start = get_date($1('#period-start').value),
			    end   = get_date($1('#period-end').value)

			switch (this.value) {
				case 'day-b':     start.setDate(start.getDate()     - 1); end.setDate(end.getDate()     - 1); break;
				case 'week-b':    start.setDate(start.getDate()     - 7); end.setDate(end.getDate()     - 7); break;
				case 'month-b':   start.setMonth(start.getMonth()   - 1); end.setMonth(end.getMonth()   - 1); break;
				case 'year-b':    start.setYear(start.getFullYear() - 1); end.setYear(end.getFullYear() - 1); break;
				case 'day-f':     start.setDate(start.getDate()     + 1); end.setDate(end.getDate()     + 1); break;
				case 'week-f':    start.setDate(start.getDate()     + 7); end.setDate(end.getDate()     + 7); break;
				case 'month-f':   start.setMonth(start.getMonth()   + 1); end.setMonth(end.getMonth()   + 1); break;
				case 'year-f':    start.setYear(start.getFullYear() + 1); end.setYear(end.getFullYear() + 1); break;
			}
			if (start.getDate() === 1 && this.value.substr(0, 5) === 'month')
				end = new Date(start.getFullYear(), start.getMonth() + 1, 0)

			$1('#dash-select-period').className = ''
			set_period(start, end);
		})
	}

	// Setup datepicker fields.
	var hdr_datepicker = function() {
		on('#dash-form', 'submit', function(e) {
			// Remove the "off" checkbox placeholders.
			$$('#dash-form :checked').forEach((c) => {
				$$(`input[name="${c.name}"][value="off"]`).forEach((i) => { i.disabled = true })
			})

			if (get_date($1('#period-start').value) <= get_date($1('#period-end').value))
				return

			e.preventDefault()
			let end = $1('#period-end')
			if (!end.classList.contains('red')) {
				end.classList.add('red')
				end.insertAdjacentHTML('afterend', ' <span class="red">' + T('error/date-mismatch') + '</span>')
			}
		})

		// Date inputs emit change while the year is still being typed.
		on('#period-start, #period-end', 'blur', function() {
			if (this.value && this.value !== this.defaultValue)
				this.form.requestSubmit()
		})
	}

	// Reload the dashboard when typing in the filter input, so the user won't
	// have to press "enter".
	let hdr_filter = () => {
		let styled = $1('#filter-styled'),
			input  = $1('#filter-paths'),
			hl     = (v) => {
				styled.innerHTML = escape_html(input.value).replace(filter_kw, '<em>$1</em>').replace(/ /g, '&nbsp;')
				input.style.width = input.offsetWidth + 'px'
			}
		hl()
		highlight_filter()

		let showMore = () => {
			let t = $1('#filter-help-more'),
				d = $1('#filter-help div')
			if (is_visible(d)) {
				t.textContent = T('nav-dash/filter-more-help')
				d.style.display = 'none'
			}
			else {
				t.textContent = T('nav-fash/filter-less-help')
				d.style.display = 'block'
			}
		}

		on('#filter-help-more', 'click', (e) => {
			e.preventDefault()
			input.focus()
			showMore()

			let v = localStorage.getItem('filter-detailed-help') === 'true'
			localStorage.setItem('filter-detailed-help', !v)
		})

		on(input, 'keydown', (e) => {
			if (e.keyCode === 13)  // Don't submit form on enter.
				e.preventDefault()
		})

		var hide
		on(input, 'focus', (e) => {
			clearTimeout(hide)
			$1('#filter-wrap').classList.add('focus')
			if (localStorage.getItem('filter-detailed-help') === 'true' && $1('#filter-help div').style.display !== 'block')
				showMore()
			$1('#filter-help').style.display = 'block'
		})
		on(input, 'blur', (e) => {
			// Add brief timeout on the hide so that clicking "more" won't
			// trigger the blur, as the blur is triggered before the click.
			clearTimeout(hide)
			hide = setTimeout(() => {
				$1('#filter-wrap').classList.remove('focus')
				$1('#filter-help').style.display = 'none'
			}, 200)
		})

		let t
		on(input, 'input', (e) => {
			clearTimeout(t)
			hl()

			t = setTimeout(() => {
				let filter = e.target.value
				push_query({filter: filter, showrefs: null})
				$1('#filter-wrap').classList.toggle('value', filter !== '')

				let loading = document.createElement('span')
				loading.className = 'loading'
				e.target.insertAdjacentElement('afterend', loading)

				// Known limitation: push_query() only rewrites the URL, so going
				// back doesn't restore the previous filter (no popstate handler).
				reload_dashboard(() => loading.remove())
			}, 300)
		})
	}

	// Save current view.
	var metric_chart = null
	var metric_chart_bound = false

	var redraw_metric_chart = function() {
		metric_chart?.destroy()
		metric_chart = null

		let c = $1('.totals .chart[data-series]'),
			canvas = c && $1('canvas', c)
		if (!canvas)
			return

		metric_chart = draw_metric_chart(c, canvas, JSON.parse(c.dataset.series))
	}

	var init_metric_chart = function() {
		redraw_metric_chart()
		if (metric_chart_bound)
			return
		metric_chart_bound = true

		window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', redraw_metric_chart)
		new MutationObserver(function(mutations) {
			if (mutations.some((m) => m.type === 'attributes' && m.attributeName === 'class'))
				redraw_metric_chart()
		}).observe(document.documentElement, {attributes: true})
	}

	var draw_metric_chart = function(c, canvas, series) {
		let metric = c.dataset.metric,
			points = series.points,
			line = style('chart-line'),
			fill = style('chart-fill'),
			grid = style('chart-grid'),
			muted = style('muted-text'),
			ctx = canvas.getContext('2d'),
			chart = new Chart(ctx, {
				type: 'line',
				data: {
					labels: points.map((point) => metric_point_label(point, series.group)),
					datasets: [{
						data: points.map((point) => point[metric]),
						borderColor: line,
						backgroundColor: fill,
						borderWidth: 1.25,
						fill: true,
						pointRadius: 0,
						pointHoverRadius: 3,
						pointHitRadius: 12,
						tension: .18,
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
								label: (item) => format_metric(metric, item.parsed.y),
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
								precision: metric === 'visits' || metric === 'pageviews' ? 0 : undefined,
								callback: (value) => format_metric_tick(metric, value),
							},
						},
					},
				},
			})

		return chart
	}

	var metric_point_label = function(point, group) {
		if (group === 'year')
			return point.day.slice(0, 4)
		if (group === 'hour')
			return `${format_date(point.day, true)} ${String(point.hour ?? 0).padStart(2, '0')}:00`
		if (group === 'week') {
			let end = get_date(point.day)
			end.setDate(end.getDate() + 6)
			return `${format_date(point.day, true)} – ${format_date(end, true)}`
		}
		if (group === 'month') {
			let date = get_date(point.day)
			return `${monthsShort[date.getMonth()]} ${date.getFullYear()}`
		}
		return format_date(point.day, true)
	}

	var format_metric_tick = function(metric, value) {
		if (metric === 'bounce_rate')
			return `${value}%`
		if (metric === 'visit_duration')
			return format_duration(value, false)
		if (metric === 'views_per_visit')
			return Number(value).toFixed(1)
		return format_int(Math.round(value))
	}

	var format_metric = function(metric, value) {
		if (metric === 'visits' || metric === 'pageviews')
			return `${format_int(Math.round(value))} ${metric === 'visits' ? 'visits' : 'pageviews'}`
		if (metric === 'views_per_visit')
			return `${value.toFixed(2)} views per visit`
		if (metric === 'bounce_rate')
			return `${value.toFixed(0)}% bounce rate`

		return `${format_duration(value, true)} visit duration`
	}

	var format_duration = function(value, includeSeconds) {
		let seconds = Math.round(value),
			hours = Math.floor(seconds / 3600),
			minutes = Math.floor((seconds % 3600) / 60),
			parts = []
		if (hours) parts.push(`${hours}h`)
		if (minutes) parts.push(`${minutes}m`)
		if (includeSeconds || parts.length === 0) parts.push(`${seconds % 60}s`)
		return parts.join(' ')
	}

	// Paginate the main path overview.
	var paginate_pages = function() {
		let sz = $$('.pages-list .rows.pages > [data-id]').length
		on('.pages-list >.load-btns .load-less', 'click', function(e) {
			e.preventDefault()
			$$('.pages-list .rows.pages > [data-id]').slice(sz).forEach((r) => r.remove())
			this.style.display = 'none'
			let more = this.previousElementSibling
			if (more?.classList.contains('load-more'))
				more.style.display = 'inline'
		})

		on('.pages-list >.load-btns .load-more', 'click', function(e) {
			e.preventDefault()

			let btn   = this,
				next  = btn.nextElementSibling,
				less  = next?.classList.contains('load-less') ? next : null,
				pages = btn.closest('.pages-list')
			let done = paginate_button(btn, () => {
				ajax(`${BASE_PATH}/load-widget`, {
					data: append_period({
						widget:    pages.getAttribute('data-widget'),
						group:     $1('#hl-group').value,
						exclude:   $$('.count-list-pages .rows.pages > [data-id]', pages).map((e) => e.dataset.id).join(','),
						max:       get_original_scale(),
						total:     get_total(),
					}),
					success: function(data) {
						if (less)
							less.style.display = 'inline'
						$1('.count-list-pages .rows.pages', pages).insertAdjacentHTML('beforeend', data.html)

						// Update scale in case it's higher than the previous maximum value.
						if (data.max > get_original_scale()) {
							$$('.count-list-pages').forEach((e) => {
								e.setAttribute('data-max', data.max)
							})
						}

						highlight_filter()
						btn.style.display = data.more ? 'inline-block' : 'none'

						$$('.total-display', pages).forEach((t) => {
							t.textContent = format_int(parseInt(t.textContent.replace(/[^0-9]/, ''), 10) + data.total_display)
						})

						done()
					},
				})
			})
		})
	}

	// Paginate and show details for the horizontal charts.
	var hchart_detail = function() {
		// Paginate the horizontal charts.
		on('.hcharts', 'click', '.hchart > .load-less', function(e) {
			e.preventDefault()
			let rows = $1('.rows', this.closest('.hchart')),
				sz   = rows._pagesize || 6
			child_divs(rows).slice(sz).forEach((r) => r.remove())
			this.style.display = 'none'
			let more = this.previousElementSibling
			if (more?.classList.contains('load-more'))
				more.style.display = 'inline'
		})

		on('.hcharts', 'click', '.hchart > .load-more', function(e) {
			e.preventDefault();

			let btn   = this,
				next  = btn.nextElementSibling,
				less  = next?.classList.contains('load-less') ? next : null,
				chart = btn.closest('.hchart'),
				key   = chart.getAttribute('data-key'),
				rows  = Array.from(chart.children).find((c) => c.classList.contains('rows'))
			if (rows && !rows._pagesize)
				rows._pagesize = rows.children.length
			let done = paginate_button(btn, () => {
				ajax(`${BASE_PATH}/load-widget`, {
					data: append_period({
						widget: chart.getAttribute('data-widget'),
						total:  chart.getAttribute('data-total') || get_total(),
						key:    key,
						offset: child_divs(rows).filter((d) => !d.classList.contains('hchart')).length,
					}),
					success: function(data) {
						if (less)
							less.style.display = 'inline'
						parse_rows(data.html).forEach((d) => rows.appendChild(d))
						if (!data.more)
							btn.style.display = 'none'
						done()
					},
				})
			})
		})

		// Load detail.
		on('.hchart', 'click', '.load-detail', function(e) {
			e.preventDefault()

			var btn    = this,
				row    = btn.closest('div[data-key]'),
				chart  = row.closest('.hchart'),
				widget = chart.getAttribute('data-widget'),
				key    = row.getAttribute('data-key'),
				isPage = row.hasAttribute('data-id'),
				total  = row.getAttribute('data-detail-total') || get_total()
			if (row.nextElementSibling?.classList.contains('detail')) {
				row.nextElementSibling.remove()
				row.classList.remove('target')
				if (isPage)
					push_query({showrefs: null})
				return
			}
			if (isPage)
				push_query({showrefs: key})

			var l = $1('.bar-c', btn)
			l?.classList.add('loading')
			var done = paginate_button(l, () => {
				ajax(BASE_PATH + '/load-widget', {
					data: append_period({
						widget: widget,
						key:    key,
						total:  total,
						//offset: rows.find('>div').length,
					}),
					success: function(data) {
						$$('.detail', chart).forEach((d) => d.remove())
						$$('.target', chart).forEach((d) => d.classList.remove('target'))
						let detail = document.createElement('div')
						detail.className = 'hchart detail'
						detail.setAttribute('data-widget', widget)
						detail.setAttribute('data-key', key)
						detail.setAttribute('data-total', total)
						detail.innerHTML = data.html
						row.insertAdjacentElement('afterend', detail)
						row.classList.add('target')
						done()
					},
				})
			})
		})
	}

	// Parse all query parameters from string to {k: v} object.
	var split_query = function(s) {
		s = s.substr(s.indexOf('?') + 1);
		if (s.length === 0)
			return {};

		var split = s.split('&'),
			obj = {};
		for (var i = 0; i < split.length; i++) {
			var item = split[i].split('=');
			obj[item[0]] = decodeURIComponent(item[1]);
		}
		return obj;
	}

	// Join query parameters from {k: v} object to href.
	var join_query = function(obj) {
		var s = [];
		for (var k in obj)
			s.push(k + '=' + encodeURIComponent(obj[k]));
		return (s.length === 0 ? location.pathname : ('?' + s.join('&')));
	}

	// Set one query parameter – leaving the others alone – and push to history.
	var push_query = function(params) {
		var current = split_query(location.search)
		for (var k in params) {
			if (params[k] === null)
				delete current[k]
			else
				current[k] = params[k]
		}
		history.pushState(null, '', join_query(current))
	}
	
	// Quote special regexp characters. https://locutus.io/php/pcre/preg_quote/
	var quote_re = (s) => s.replace(new RegExp('[.\\\\+*?\\[\\^\\]$(){}=!<>|:\\-]', 'g'), '\\$&')
})();
