import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import {runInNewContext} from 'node:vm'

const source = readFileSync(new URL('./count.js', import.meta.url), 'utf8')

function load({page = 'https://example.com/docs?ref=newsletter#intro', dataset = {},
	settings = {}, visibility = 'visible', frame = false, beacon = true, webdriver = false} = {}) {
	const requests = []
	const errors = []
	const document = Object.assign(new EventTarget(), {
		currentScript: {src: 'https://stats.example.com/analytics/count.js?v=2',
			dataset: {site: 'example.com', ...dataset}},
		baseURI: page,
		referrer: 'https://search.example/query?q=docs',
		visibilityState: visibility,
		prerendering: visibility === 'prerender',
	})
	const window = {goatcounter: settings, screen: {width: 1440}}
	window.self = window
	window.top = frame ? {} : window
	const navigator = {webdriver}
	if (beacon !== null)
		navigator.sendBeacon = (url) => {
			if (beacon instanceof Error)
				throw beacon
			if (beacon)
				requests.push({type: 'beacon', url: new URL(url)})
			return beacon
		}
	class Image {
		set src(url) { requests.push({type: 'image', url: new URL(url)}) }
	}
	const location = new URL(page)
	runInNewContext(source, {window, document, navigator, location, URL, Image,
		console: {error: (...args) => errors.push(args)}})
	return {requests, errors, counter: window.goatcounter, location, document,
		show(state) {
			document.visibilityState = state
			document.prerendering = state === 'prerender'
			document.dispatchEvent(new Event('visibilitychange'))
		}}
}

test('logs an error and sends nothing when no site can be determined', () => {
	const tracker = load({dataset: {site: ''}})
	assert.equal(tracker.requests.length, 0)
	assert.equal(tracker.errors.length, 1)
	assert.match(tracker.errors[0][0], /missing site/)

	tracker.counter.count({site: 'manual.example'})
	assert.equal(tracker.requests.length, 1)
	assert.equal(tracker.requests[0].url.searchParams.get('site'), 'manual.example')
})

test('counts a pageview with the collection fields and script-relative endpoint', () => {
	const {requests} = load()
	assert.equal(requests.length, 1)
	const {url, type} = requests[0]
	assert.equal(type, 'beacon')
	assert.equal(url.origin + url.pathname, 'https://stats.example.com/analytics/count')
	const data = Object.fromEntries(url.searchParams)
	assert.ok(data.rnd)
	delete data.rnd
	assert.deepEqual(data, {
		site: 'example.com', p: '/docs?ref=newsletter',
		r: 'https://search.example/query?q=docs', e: 'false', ns: 'false',
		s: '1440', b: '0',
	})
})

test('resolves endpoint overrides against the page and preserves their query', () => {
	for (const endpoint of ['/events?token=abc', '//other.example/events?token=abc']) {
		const {requests} = load({dataset: {endpoint}, settings: {endpoint: '/unused'}})
		const {url} = requests[0]
		const expected = new URL(endpoint, 'https://example.com')
		assert.equal(url.origin + url.pathname, expected.origin + expected.pathname)
		assert.equal(url.searchParams.get('token'), 'abc')
	}
	assert.equal(load({settings: {endpoint: '/custom'}}).requests[0].url.pathname, '/custom')
})

test('manual events and pageviews use current values without leaking previous options', () => {
	const {counter, requests, location} = load({settings: {no_onload: true}})
	assert.equal(requests.length, 0)
	counter.count({path: 'Download & share', event: true, referrer: '', no_session: true, site: 'other.example'})
	const event = requests[0].url.searchParams
	assert.equal(event.get('p'), 'Download & share')
	assert.equal(event.get('e'), 'true')
	assert.equal(event.get('r'), '')
	assert.equal(event.get('ns'), 'true')
	assert.equal(event.get('site'), 'other.example')
	location.href = 'https://example.com/next?ref=next#section'
	counter.count()
	const page = requests[1].url.searchParams
	assert.equal(page.get('p'), '/next?ref=next')
	assert.equal(page.has('q'), false)
	assert.equal(page.get('e'), 'false')
	assert.equal(page.get('ns'), 'false')
	assert.equal(page.get('site'), 'example.com')
})

test('waits for visibility and counts the initial page only once', () => {
	for (const visibility of ['hidden', 'prerender']) {
		const tracker = load({visibility})
		assert.equal(tracker.requests.length, 0)
		tracker.show('hidden')
		assert.equal(tracker.requests.length, 0)
		tracker.show('visible')
		tracker.show('hidden')
		tracker.show('visible')
		assert.equal(tracker.requests.length, 1)
	}
})

test('falls back to an image when beacons are unavailable, rejected, or throw', () => {
	for (const beacon of [null, false, new Error('blocked')]) {
		const {requests} = load({beacon})
		assert.equal(requests.length, 1)
		assert.equal(requests[0].type, 'image')
		assert.equal(requests[0].url.searchParams.get('p'), '/docs?ref=newsletter')
	}
})

test('skips local pages and frames unless explicitly allowed', () => {
	for (const page of ['file:///tmp/test.html', 'http://localhost', 'http://app.localhost',
		'http://127.0.0.1', 'http://[::1]', 'http://10.0.0.1', 'http://172.16.0.1',
		'http://172.31.255.255', 'http://192.168.1.1', 'http://0.0.0.0']) {
		assert.equal(load({page}).requests.length, 0, page)
		assert.equal(load({page, settings: {allow_local: true}}).requests.length, 1, page)
	}
	assert.equal(load({frame: true}).requests.length, 0)
	assert.equal(load({frame: true, settings: {allow_frame: true}}).requests.length, 1)
	for (const page of ['https://notlocalhost.com', 'http://172.15.0.1', 'http://172.32.0.1'])
		assert.equal(load({page}).requests.length, 1, page)
})

test('marks automated browsers and refuses manual counts during prerendering', () => {
	assert.equal(load({webdriver: true}).requests[0].url.searchParams.get('b'), '153')
	const {counter, requests} = load({visibility: 'prerender', settings: {no_onload: true}})
	counter.count()
	assert.equal(requests.length, 0)
})
