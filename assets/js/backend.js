import './dashboard.js'
import {$$, $1, ajax, on} from './helper.js'

;(function() {
	'use strict';

	document.addEventListener('DOMContentLoaded', function() {
		let s = $1('#js-settings')
		window.BASE_PATH         = s.getAttribute('data-base-path') || ""
		window.CSRF              = s.getAttribute('data-csrf')
		window.TZ_OFFSET         = parseInt(s.getAttribute('data-offset'), 10) || 0
		window.DEV               = s.getAttribute('data-dev') === 'true'

		;[report_errors, bind_site_selector, bind_tracking_code, onetime].forEach((f) => f.call())
		;[window.page_dashboard]
			.forEach((f) => document.body.id.match(new RegExp('^' + f.name.replace(/_/g, '-'))) && f.call())
	})

	// Set up error reporting.
	let report_errors = function() {
		if (window.DEV)
			return
		window.onerror = on_error
		window.addEventListener('unhandledrejection', (e) => on_error(`unhandled rejection: ${e.reason}`, location+'', 0, 0, e.reason))

		document.addEventListener('ajaxerror', function(e) {
			let {url, error} = e.detail
			if (url === BASE_PATH + '/jserr')  // Just in case, otherwise we'll be stuck.
				return
			if (url === BASE_PATH + '/load-widget')
				return
			let msg = `Could not load ${url}: ${error}`
			console.error(msg)
			on_error(`ajaxError: ${msg}`, url)
			alert(msg)
		})
	}

	// Report an error.
	let on_error = function(msg, url, line, column, err) {
		if (
			// Useless Safari error: https://bugs.webkit.org/show_bug.cgi?id=132945
			msg === 'Script error.' ||

			// Various crappy extensions or injected scripts keeps spamming
			// these. I'm quite sure it's not GoatCounter.
			msg.indexOf("document.getElementsByTagName('video')[0].webkitExitFullScreen") !== -1 ||
			msg.match(/Cannot redefine property: (googletag|ethereum)/) !== null ||
			msg.indexOf('ResizeObserver loop completed with undelivered notifications') !== -1 ||
			msg.indexOf("Can't find variable: requestIdleCallback") !== -1 ||
			msg.indexOf('Permission denied to access property "apply"') !== 1 ||

			// Only from bot, never any details.
			msg.indexOf('Exception invoking lineTo') !== -1
		)
			return

		let stack = typeof(err?.stack) === 'string' ? err.stack : ''

		// Don't log errors from extensions.
		if (url.startsWith('chrome-extension://') || stack.indexOf('@moz-extension://') !== -1 ||
			stack.indexOf('global code@') !== -1 // Outside function; probs injected script
		)
			return

		ajax(BASE_PATH + '/jserr', {
			method: 'POST',
			data:   {msg: msg, url: url, line: line, column: column, stack: stack, ua: navigator.userAgent, loc: window.location+''},
		})
	}

	var bind_site_selector = function() {
		on('#site-selector', 'change', function() {
			let url = new URL(location.href)
			url.searchParams.set('site', this.value)
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

	// One-time messages.
	let onetime = function() {
		$$('.onetime').forEach((elem) => {
			let n = elem.className.split(' ').filter((v) => v.match(/^onetime-/))[0]
			if (localStorage.getItem(n))
				return

			elem.style.display = 'block'
			on($$('.close', elem), 'click', (e) => {
				e.preventDefault()
				elem.style.display = 'none'
				localStorage.setItem(n, '1')
			})
		})
	}

})();
