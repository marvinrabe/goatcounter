;(() => {
	const script = document.currentScript
	if (!script)
		return

	const counter = window.goatcounter = window.goatcounter || {}
	const endpoint = script.dataset.endpoint || counter.endpoint || new URL('count', script.src).href

	counter.count = (options = {}) => {
		const site = options.site != null ? options.site : script.dataset.site
		if (!site) {
			console.error('GoatCounter: missing site; set data-site on the tracking script or pass site to count()')
			return
		}

		if (document.prerendering || document.visibilityState === 'prerender')
			return
		if (!counter.allow_frame && window.self !== window.top)
			return
		if (!counter.allow_local && (location.protocol === 'file:' ||
			/(^|\.)localhost$|^127\.|^10\.|^172\.(1[6-9]|2\d|3[01])\.|^192\.168\.|^0\.0\.0\.0$|^\[::1\]$/.test(location.hostname)))
			return

		const url = new URL(endpoint, document.baseURI)
		const data = {
			site,
			p: options.path != null ? options.path : location.pathname + location.search,
			r: options.referrer != null ? options.referrer : document.referrer,
			h: location.hostname,
			n: options.event || '',
			pr: options.event && options.props ? JSON.stringify(options.props) : '',
			ns: !!options.no_session,
			b: navigator.webdriver ? 153 : 0,
		}
		for (const [key, value] of Object.entries(data))
			url.searchParams.set(key, value)

		try {
			if (navigator.sendBeacon(url.href))
				return
		} catch (_) {
			// Fall back to fetch() if the beacon is blocked or fails.
		}
		fetch(url.href, {method: 'POST', keepalive: true, mode: 'no-cors'}).catch(() => {})
	}

	// Count clicks on links to other sites, as Plausible's outbound link
	// tracking does.
	if (!counter.no_outbound) {
		document.addEventListener('click', (event) => {
			const link = event.target?.closest?.('a[href]')
			if (!link || !/^https?:$/.test(link.protocol) || link.host === location.host)
				return
			counter.count({event: 'Outbound Link: Click', props: {url: link.href}})
		}, true)
	}

	if (!counter.no_onload) {
		let initialCounted = false
		const count = () => {
			if (document.visibilityState !== 'visible' || document.prerendering)
				return
			document.removeEventListener('visibilitychange', count)
			initialCounted = true
			counter.count()
		}
		document.addEventListener('visibilitychange', count)
		window.addEventListener('pageshow', (event) => {
			if (!event.persisted || document.prerendering || document.visibilityState === 'prerender')
				return
			if (!initialCounted) {
				document.removeEventListener('visibilitychange', count)
				initialCounted = true
			}
			counter.count()
		})
		count()
	}
})()
