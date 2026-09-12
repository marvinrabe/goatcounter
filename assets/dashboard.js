import {
	$$,
	$1,
	T,
	ajax,
	days,
	daysShort,
	format_date,
	format_date_ymd,
	format_int,
	get_date,
	is_visible,
	months,
	monthsShort,
	on,
	paginate_button,
	parse_html,
	parse_rows,
	style,
} from './helper.js'

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
		;[init_charts, paginate_pages, load_refs, hchart_detail, ref_pages, bind_scale].forEach((f) => f.call())
	}

	// Direct element children of elem, which is what jQuery's ">div" did.
	var child_divs = (elem) => Array.from(elem.children).filter((c) => c.tagName === 'DIV')

	// Escape HTML special characters.
	var escape_html = (s) => s.replace(/[&<>"]/g, (c) => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;'})[c])

	// Get the Y-axis scale.
	var get_original_scale = function() { return parseInt($1('.count-list-pages')?.getAttribute('data-max'), 10) }
	var get_current_scale  = function() { return parseInt($1('.count-list-pages')?.getAttribute('data-scale'), 10) }

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
				redraw_all_charts()
				highlight_filter()
				if (done)
					done()
			},
		})
	}

	// Append period-start and period-end values to the data object.
	var append_period = function(data) {
		data = data || {}
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

	let filter_kw = /(\b(?:at:start|at:end|is:event|is:pageview|in:path|in:title)|\B:not)\b/g

	// Highlight a filter pattern in the path and title.
	let highlight_filter = () => {
		let val = $1('#filter-paths').value,
			s   = val.replace(filter_kw, '').trim()
		if (s === '')
			return

		let start   = '',
			end     = '',
			inPath  = false,
			inTitle = false,
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
			else if (k === 'in:title')
				inTitle = true
		}
		if (!inPath && !inTitle)
			[inPath, inTitle] = [true, true]
		let where = []
		if (inPath)
			where.push('.rlink')
		if (inTitle)
			where.push('.page-title:not(.no-title)')

		$$('.pages-list .count-list-pages > tbody.pages').forEach((tbody) => {
			$$(where.join(','), tbody).forEach((elem) => {
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

			if (this.value.substr(-2) === '-f' && end.getTime() > (new Date()).getTime())
				return alert(T('error/date-future'))

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

			if (start > (new Date()).getTime())
				return alert(T('error/date-future'))
			if (SITE_FIRST_HIT_AT > end.getTime())
				return alert(T('error/date-past'))

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

		on('#period-start, #period-end', 'change', () => { $1('#dash-form').requestSubmit() })
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
	// Load pages for reference in Totals
	var ref_pages = function() {
		on('.count-list', 'click', '.pages-by-ref', function(e) {
			e.preventDefault()
			var btn = this,
				p   = btn.parentNode

			if ($1('.list-ref-pages', p)) {
				$$('.list-ref-pages', p).forEach((e) => e.remove())
				return
			}

			$$('.list-ref-pages').forEach((e) => e.remove())
			var done = paginate_button(btn, () => {
				ajax(BASE_PATH + '/pages-by-ref', {
					data: append_period({name: btn.textContent}),
					success: function(data) {
						p.insertAdjacentHTML('beforeend', data.html)
						done()
					}
				})
			})
		})
	}

	// Keep an array of charts so we can stop them on resize, otherwise the
	// resize and mouse event will be bound twice.
	var charts = []

	// Bind the Y-axis scale actions.
	var bind_scale = function() {
		on('.count-list', 'click', '.rescale', function(e) {
			e.preventDefault()

			var scale = this.closest('.chart').getAttribute('data-max')
			$$('.pages-list .scale').forEach((e) => { e.innerHTML = format_int(scale) })
			$$('.pages-list .count-list-pages').forEach((e) => e.setAttribute('data-scale', scale))

			charts.forEach((c) => {
				c.ctx().canvas.dataset.done = ''
				c.stop()
			})
			charts = []
			draw_all_charts()
		})
	}

	var redraw_all_charts = function() {
		$1('#tooltip')?.remove()
		charts.forEach((c) => {
			c.ctx().canvas.dataset.done = ''
			c.stop()
		})
		charts = []
		draw_all_charts()
	}

	// Only bind the global redraw listeners once, since this runs again after
	// every widget reload.
	var charts_bound = false

	var init_charts = function() {
		draw_all_charts()

		if (charts_bound)
			return
		charts_bound = true

		window.addEventListener('resize', redraw_all_charts)
		window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', redraw_all_charts)

		// force-dark manually added or removed.
		new MutationObserver(function(muts, observer) {
			muts.forEach((m) => m.type === 'attributes' && m.attributeName === 'class' && redraw_all_charts())
		}).observe(document.documentElement, {attributes: true})
	}

	// Draw all charts.
	var draw_all_charts = function() {
		$$('.chart-line, .chart-bar').forEach(function(chart) {
			// Use setTimeout to force the browser to actually render this ASAP;
			// without it, the charts will all be displayed at the same time,
			// rather than one-by-one as they're generated.
			//
			// It's not super-slow to make them, but it's just a split second
			// where the chart is blank, and it's better with this, especially
			// on long timeviews and/or with many pages displayed at once.
			setTimeout(() => draw_chart(chart), 0)
		})
	}

	// Draw this chart
	var draw_chart = function(c) {
		let canvas = $1('canvas', c)
		if (!canvas || canvas.dataset.done === 't')
			return
		canvas.dataset.done = 't'

		let stats = JSON.parse(c.dataset.stats)
		if (!stats)
			return

		let ctx     = canvas.getContext('2d', {alpha: false}),
			max     = Math.max(10, parseInt(c.dataset.max, 10)),
			scale   = get_current_scale(),
			hourly  = c.dataset.group === 'hour',
			daily   = c.dataset.group === 'day',
			weekly  = c.dataset.group === 'week',
			monthly = c.dataset.group === 'month',
			isBar   = c.classList.contains('chart-bar'),
			isEvent = !!c.closest('tr')?.classList.contains('event'),
			isPages = !!c.closest('.count-list-pages'),
			ndays   = (get_date($1('#period-end').value) - get_date($1('#period-start').value)) / (86400*1000)

		if (isPages && scale)
			max = scale

		var data
		if (hourly)
			data = stats.map((s) => s.hourly).reduce((a, b) => a.concat(b))
		else if (daily)
			data = stats.map((s) => [s.daily]).reduce((a, b) => a.concat(b))
		else if (weekly) {
			stats = stats.filter((v, i) => i % 7 == 0)
			data = stats.map((s) => [s.weekly]).reduce((a, b) => a.concat(b))
		}
		else if (monthly) {
			stats = stats.filter((v) => v.day.endsWith('-01'))
			data = stats.map((s) => [s.monthly]).reduce((a, b) => a.concat(b))
		}

		var chart = window.charty(ctx, data, {
			mode: isBar ? 'bar' : 'line',
			max:  max,
			line: {
				color: style('chart-line'),
				fill:  style('chart-fill'),
				width: daily || weekly || monthly || ndays <= 14 ? 1.5 : 1
			},
			bar:  {color: style('chart-line')},
		})
		charts.push(chart)

		// Show tooltip and highlight position on mouse hover.
		var tip = document.createElement('div'),
			reset = {x: -1, y: -1, f: () => {}}
		tip.id = 'tooltip'
		chart.mouse(function(i, x, y, w, h, offset, ev) {
			if (ev == 'leave') {
				tip.remove()
				reset.f()
				return
			}
			else if (ev === 'enter') { }
			else if (x === reset.x)
				return

			let day    = hourly ? stats[Math.floor(i / 24)] : stats[i],
				visits = day.hourly[i%24],
				views  = day.hourly[i%24],
				title  = ''
			if (hourly)
				title = `${format_date(day.day, true)} ${(i % 24)}:00 – ${(i % 24)}:59`
			else if (daily) {
				[visits, views] = [day.daily, day.daily]
				title = `${format_date(day.day, true)}`
			}
			else if (weekly) {
				[visits, views] = [day.weekly, day.weekly]
				let end = get_date(day.day)
				end.setDate(end.getDate() + 6)
				title = `${format_date(day.day, true)} to ${format_date(end, true)}`
			} else if (monthly) {
				[visits, views] = [day.monthly, day.monthly]
				let d = get_date(day.day)
				title = `${months[d.getMonth() % 12]} ${d.getFullYear() + Math.floor(d.getMonth() / 12)}`
			}

			if (!FEWER_NUMBERS) {
				if (isEvent) {
					title += '; ' + T('dashboard/tooltip-event', {
						unique: format_int(visits),
						clicks: `<span class="views">${format_int(views)}`,
					}) + '</span>'
				}
				else {
					title += '; ' + T('dashboard/totals/num-visits', {
						'num-visits': format_int(visits),
					}) + '</span>'
				}
			}

			tip.remove()
			tip.innerHTML = title
			document.body.appendChild(tip)
			tip.style.left = (offset.left + x) + 'px'
			tip.style.top  = (offset.top - tip.offsetHeight - 10) + 'px'
			if (tip.offsetHeight > 30) {
				tip.style.left = '0'
				tip.style.left = (x + offset.left - tip.offsetWidth - 8) + 'px'
			}

			reset.f()
			reset = chart.draw(x, 0, w, h, function() {
				ctx.strokeStyle = '#999'
				ctx.fillStyle   = 'rgba(99, 99, 99, .5)'
				ctx.lineWidth   = 1

				ctx.beginPath()
				if (isBar) {
					ctx.moveTo(x, 2.5)
					ctx.lineTo(x+w, 2.5)
					ctx.lineTo(x+w, 47.5)
					ctx.lineTo(x, 47.5)
					ctx.lineTo(x, 2.5)
					ctx.fill()
				}
				else {
					ctx.moveTo(x + ctx.lineWidth/2, 2.5)
					ctx.lineTo(x + ctx.lineWidth/2, 47.5)
					ctx.stroke()
				}
			})
		})
	}

	// Paginate the main path overview.
	var paginate_pages = function() {
		let sz = $$('.pages-list tbody >tr').length
		on('.pages-list >.load-btns .load-less', 'click', function(e) {
			e.preventDefault()
			$$('.pages-list tbody >tr').slice(sz).forEach((r) => r.remove())
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
						exclude:   $$('.count-list-pages >tbody >tr', pages).map((e) => e.dataset.id).join(','),
						max:       get_original_scale(),
					}),
					success: function(data) {
						if (less)
							less.style.display = 'inline'
						$1('.count-list-pages >tbody.pages', pages).insertAdjacentHTML('beforeend', data.html)

						// Update scale in case it's higher than the previous maximum value.
						if (data.max > get_original_scale()) {
							$$('.count-list-pages').forEach((e) => {
								e.setAttribute('data-max', data.max)
								e.setAttribute('data-scale', data.max)
							})
							$$('.count-list-pages .scale').forEach((e) => { e.textContent = data.max })
						}

						draw_all_charts()

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

	// Load references as an AJAX request.
	var load_refs = function() {
		on('.count-list-pages', 'click', '.hchart .load-less', function(e) {
			e.preventDefault()
			let rows = $1('.rows', this.closest('.hchart')),
				sz   = rows._pagesize || 10
			child_divs(rows).slice(sz).forEach((r) => r.remove())
			this.style.display = 'none'
			let more = this.previousElementSibling
			if (more?.classList.contains('load-more'))
				more.style.display = 'inline'
		})

		on('.count-list-pages', 'click', '.load-refs, .hchart .load-more', function(e) {
			e.preventDefault()

			let params = split_query(location.search),
				btn    = this,
				next   = btn.nextElementSibling,
				less   = next?.classList.contains('load-less') ? next : null,
				row    = btn.closest('tr'),
				rows   = $1('.refs .rows', row),
				widget = row.closest('.pages-list').getAttribute('data-widget'),
				path   = row.getAttribute('data-id'),
				init   = btn.classList.contains('load-refs'),
				close  = function() {
					var t = $1(`tr[data-id="${(params['showrefs'] || '').replace(/(["\\])/g, '\\$1')}"]`)
					if (!t)
						return
					t.classList.remove('target')
					let refs = $1('.refs', t.closest('tr'))
					if (refs)
						refs.innerHTML = ''
				}
			if (rows && !rows._pagesize)
				rows._pagesize = rows.children.length

			// Clicked on row that's already open, so close and stop. Don't
			// close anything yet if we're going to load another path, since
			// that gives a somewhat yanky effect (close, wait on xhr, open).
			if (init && params['showrefs'] === path) {
				close()
				return push_query({showrefs: null})
			}

			push_query({showrefs: path})
			let done = paginate_button(btn, () => {
				ajax(BASE_PATH + '/load-widget', {
					data: append_period({
						widget: widget,
						key:    path,
						total:  row.getAttribute('data-count'),
						offset: $$('.refs .rows>div', row).length,
					}),
					success: function(data) {
						if (less)
							less.style.display = 'inline'
						row.classList.add('target')

						if (init) {
							if (params['showrefs'])
								close()
							$1('.refs', row).innerHTML = data.html
						}
						else {
							parse_rows(data.html).forEach((d) => rows.appendChild(d))
							if (!data.more)
								btn.style.display = 'none'
						}
						done()
					},
				})
			})
		})
	}

	// Paginate and show details for the horizontal charts.
	var hchart_detail = function() {
		// Paginate the horizontal charts.
		on('.hcharts', 'click', '.load-less', function(e) {
			e.preventDefault()
			let rows = $1('.rows', this.closest('.hchart')),
				sz   = rows._pagesize || 6
			child_divs(rows).slice(sz).forEach((r) => r.remove())
			this.style.display = 'none'
			let more = this.previousElementSibling
			if (more?.classList.contains('load-more'))
				more.style.display = 'inline'
		})

		on('.hcharts', 'click', '.load-more', function(e) {
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
						total:  get_total(),
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
				key    = row.getAttribute('data-key')
			if (row.nextElementSibling?.classList.contains('detail'))
				return row.nextElementSibling.remove()

			var l = $1('.bar-c', btn)
			l?.classList.add('loading')
			var done = paginate_button(l, () => {
				ajax(BASE_PATH + '/load-widget', {
					data: append_period({
						widget: widget,
						key:    key,
						total:  get_total(),
						//offset: rows.find('>div').length,
					}),
					success: function(data) {
						$$('.detail', chart).forEach((d) => d.remove())
						let detail = document.createElement('div')
						detail.className = 'hchart detail'
						detail.setAttribute('data-widget', widget)
						detail.setAttribute('data-key', key)
						detail.innerHTML = data.html
						row.insertAdjacentElement('afterend', detail)
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
