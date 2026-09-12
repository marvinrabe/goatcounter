import './charty.js'
import './dashboard.js'
import {$$, $1, T, ajax, on} from './helper.js'

;(function() {
	'use strict';

	document.addEventListener('DOMContentLoaded', function() {
		let s = $1('#js-settings')
		window.I18N              = JSON.parse($1('#js-i18n').textContent)
		window.BASE_PATH         = s.getAttribute('data-base-path') || ""
		window.CSRF              = s.getAttribute('data-csrf')
		window.TZ_OFFSET         = parseInt(s.getAttribute('data-offset'), 10) || 0
		window.SITE_FIRST_HIT_AT = s.getAttribute('data-first-hit-at') * 1000
		window.DEV               = s.getAttribute('data-dev') === 'true'
		window.FEWER_NUMBERS     = s.getAttribute('data-fewer-numbers') === 'true'

		;[report_errors, bind_tooltip, bind_confirm, onetime].forEach((f) => f.call())
		;[window.page_dashboard, page_settings_main]
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
			let msg = T("error/load-url", {url: url, error: error})
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

	// Show confirmation on everything with data-confirm.
	var bind_confirm = function() {
		on(document.body, 'click submit', '[data-confirm]', function(e) {
			if (e.type === 'click' && this.tagName === 'FORM')
				return
			if (!confirm(this.getAttribute('data-confirm')))
				e.preventDefault()
		})
	}

	// Show custom tooltip on everything with a title attribute.
	var bind_tooltip = function() {
		var tip = document.createElement('div')
		tip.id = 'tooltip'

		var display = function(e, t) {
			if (t.classList.contains('rlink') && t.offsetWidth >= t.scrollWidth)
				return

			tip.remove()
			tip.innerHTML = t.getAttribute('data-title')
			tip.style.left = e.pageX + 'px'
			tip.style.top  = (e.pageY + 20) + 'px'
			t.addEventListener('mouseleave', () => { tip.remove() }, {once: true})
			document.body.appendChild(tip)
			if (tip.offsetHeight > 30) {  // Move to left if there isn't enough space.
				tip.style.left = '0'
				tip.style.left = (e.pageX - tip.offsetWidth - 8) + 'px'
			}
		}

		on(document.body, 'mouseenter', '[data-title]', function(e) {
			display(e, this)
		})

		on(document.body, 'mouseenter', '[title]', function(e) {
			var title = this.getAttribute('title')

			this.setAttribute('data-title', title)
			this.removeAttribute('title')
			display(e, this)
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

	var page_settings_main = function() {
		// Add current IP address to ignore_ips.
		on('#add-ip', 'click', function(e) {
			e.preventDefault()

			ajax(BASE_PATH + '/settings/main/ip', {
				success: function(data) {
					var input   = $1('[name="settings.ignore_ips"]'),
						current = input.value.split(',').
							map(function(m) { return m.trim() }).
							filter(function(m) { return m !== '' })

					if (current.indexOf(data) > -1) {
						$1('#add-ip').insertAdjacentHTML('afterend',
							'<span class="err">IP ' + data + ' is already in the list</span>')
						return
					}
					current.push(data)
					var set = current.join(', ')
					input.value = set
					input.focus()
					input.setSelectionRange(set.length, set.length)
				},
			})
		})

		// Generate random token.
		on('#rnd-secret', 'click', function(e) {
			e.preventDefault()
			let secret = $1('#settings-secret')
			secret.value = Array.from(window.crypto.getRandomValues(new Uint8Array(20)), (c) => c.toString(36)).join('')
			secret.dispatchEvent(new Event('change'))
		})

		// Show secret token.
		on('#settings-public', 'change', function(e) {
			$1('#secret').style.display = this.value === 'secret' ? 'block' : 'none'
			if ($1('#settings-secret').value === '')
				$1('#rnd-secret').click()
		})
		$1('#settings-public')?.dispatchEvent(new Event('change'))

		// Update redirect link.
		on('#settings-secret', 'change', function(e) {
			$1('#secret-url').value = `${location.protocol}//${location.host}${BASE_PATH}?access-token=${this.value}`
		})
		$1('#settings-secret')?.dispatchEvent(new Event('change'))
	}

})();
