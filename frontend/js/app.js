import {page_dashboard} from './dashboard.js'
import {$1, on} from './dom.js'

;(function() {
	'use strict';

	document.addEventListener('DOMContentLoaded', function() {
		;[report_ajax_errors, bind_site_selector, bind_tracking_code].forEach((f) => f.call())
		if (document.body.id === 'page-dashboard')
			page_dashboard()
	})

	// Requests to load more widget data fail silently; tell about the others.
	let report_ajax_errors = function() {
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
})();
