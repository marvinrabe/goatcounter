;(() => {
	const script = document.currentScript
	if (!script)
		return

	const counter = window.goatcounter = window.goatcounter || {}
	const endpoint = script.dataset.endpoint || counter.endpoint || new URL('count', script.src).href

	counter.count = (options = {}) => {
		if (document.prerendering || document.visibilityState === 'prerender')
			return
		if (!counter.allow_frame && window.self !== window.top)
			return
		if (!counter.allow_local && (location.protocol === 'file:' ||
			/(^|\.)localhost$|^127\.|^10\.|^172\.(1[6-9]|2\d|3[01])\.|^192\.168\.|^0\.0\.0\.0$|^\[::1\]$/.test(location.hostname)))
			return

		const url = new URL(endpoint, document.baseURI)
		const data = {
			site: options.site ?? script.dataset.site ?? '',
			p: options.path ?? location.pathname + location.search,
			r: options.referrer ?? document.referrer,
			e: !!options.event,
			ns: !!options.no_session,
			s: window.screen.width,
			b: navigator.webdriver ? 153 : 0,
			q: location.search,
			rnd: Math.random().toString(36).slice(2),
		}
		for (const [key, value] of Object.entries(data))
			url.searchParams.set(key, value)

		try {
			if (navigator.sendBeacon?.(url.href))
				return
		} catch {
			// A blocked beacon can still work as an image request.
		}
		new Image().src = url.href
	}

	if (!counter.no_onload) {
		const count = () => {
			if (document.visibilityState !== 'visible' || document.prerendering)
				return
			document.removeEventListener('visibilitychange', count)
			counter.count()
		}
		document.addEventListener('visibilitychange', count)
		count()
	}
})()
