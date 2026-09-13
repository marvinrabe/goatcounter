'use strict';

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

// The rows out of a "rows only" widget response, which is a div.rows wrapping
// them with the pagination links as siblings. The equivalent of what
// $(html).find('>div') used to do.
var parse_rows = (html) => Array.from(parse_html(html).children).
	flatMap((n) => Array.from(n.children)).
	filter((c) => c.tagName === 'DIV')

// Is this element visible, i.e. does it generate a layout box?
var is_visible = (elem) => !!elem && (elem.offsetWidth > 0 || elem.offsetHeight > 0 || elem.getClientRects().length > 0)

// Send an HTTP request.
//
// opt.data is an object which is sent as a query string for GET requests, and
// as a form-encoded body for anything else. opt.success is called with the
// response, parsed as JSON if the server said it's JSON.
//
// Failures are reported as an "ajaxerror" event on document, which backend.js
// listens for; errors thrown by opt.success are left alone, so that they get
// reported as a regular error rather than as a request failure.
var ajax = function(url, opt) {
	opt = opt || {}

	let method = (opt.method || 'GET').toUpperCase(),
		params = new URLSearchParams(),
		base   = url  // Without the query string, for error reporting.
	Object.entries(opt.data || {}).forEach(([k, v]) => params.set(k, v === null || v === undefined ? '' : v))

	let init = {method: method, headers: {}}
	if (method === 'GET' || method === 'HEAD') {
		let q = params.toString()
		if (q !== '')
			url += (url.indexOf('?') === -1 ? '?' : '&') + q
	}
	else {
		init.headers['Content-Type'] = 'application/x-www-form-urlencoded; charset=UTF-8'
		init.body = params.toString()
	}

	return fetch(url, init).
		then((r) => {
			if (!r.ok)
				throw new Error(`${r.status} ${r.statusText}`)
			return (r.headers.get('Content-Type') || '').startsWith('application/json') ? r.json() : r.text()
		}).
		then(
			(data) => { if (opt.success) opt.success(data) },
			(err)  => { document.dispatchEvent(new CustomEvent('ajaxerror', {detail: {url: base, error: err.message}})) })
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
	btn.classList.add('loading')
	f.call(btn)
	return () => {
		delete btn.dataset.working
		btn.classList.remove('loading')
	}
}

// Format a number with a thousands separator (a thin space, matching the
// server-side formatting). https://stackoverflow.com/a/2901298/660921
var format_int = (n) => (n+'').replace(/\B(?=(\d{3})+(?!\d))/g, '\u202f')

var months      = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'],
	days        = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'],
	monthsShort = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'],
	daysShort   = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

// Format a date as an ISO date; the year is left off if it's the current year
// and no_year_if_current is set.
var format_date = function(date, no_year_if_current) {
	if (typeof(date) === 'string')
		date = get_date(date)

	let m = date.getMonth() + 1,
		d = date.getDate(),
		s = (m >= 10 ? m : ('0' + m)) + '-' + (d >= 10 ? d : ('0' + d))
	if (no_year_if_current && date.getFullYear() === (new Date()).getFullYear())
		return s
	return date.getFullYear() + '-' + s
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

var style = function(name) {
	return getComputedStyle(document.documentElement).getPropertyValue(`--${name}`)
}

export {
	$$,
	$1,
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
}
