#!/usr/bin/env python3
"""Runs inside a disposable Kubernetes probe pod; uses only the standard library."""
import argparse
import json
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = 'http://goatcounter:8080'
DB = 'http://libsql:8080/v2/pipeline'
UA = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'


def sql(query):
    payload = {'requests': [{'type': 'execute', 'stmt': {'sql': query, 'want_rows': True}}, {'type': 'close'}]}
    req = urllib.request.Request(DB, json.dumps(payload).encode(), {'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=15) as r:
        data = json.load(r)['results'][0]
    if data['type'] == 'error':
        raise RuntimeError(data['error'])
    return [[cell.get('value') for cell in row] for row in data['response']['result']['rows']]


def number(query):
    rows = sql(query)
    return int(rows[0][0] or 0)


def hit(base, path):
    url = base + '/count?' + urllib.parse.urlencode({'site': 'example.com', 'p': path})
    req = urllib.request.Request(url, headers={'User-Agent': UA, 'Connection': 'close'})
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status
    except urllib.error.HTTPError as e:
        return e.code
    except (urllib.error.URLError, TimeoutError):
        return 0


def wait_empty():
    until = time.monotonic() + 90
    while time.monotonic() < until:
        if number('select count(*) from hit_queue') == 0:
            return
        time.sleep(.25)
    raise AssertionError('queue did not drain')


def verify(prefix, accepted, visits=None):
    wait_empty()
    clause = "p.path like '/" + prefix + "/%'"
    raw = number('select count(*) from hits h join paths p on p.path_id=h.path_id where ' + clause)
    first = number('select coalesce(sum(first_visit),0) from hits h join paths p on p.path_id=h.path_id where ' + clause)
    aggregate = number('select coalesce(sum(total),0) from hit_counts h join paths p on p.path_id=h.path_id where ' + clause)
    sessions = number('select count(distinct session) from hits h join paths p on p.path_id=h.path_id where ' + clause)
    assert raw == accepted, (prefix, raw, accepted)
    assert first == aggregate, (prefix, first, aggregate)
    if visits is not None:
        assert first == visits, (prefix, first, visits)
    assert sessions == 1, (prefix, sessions)
    return {'accepted': accepted, 'stored': raw, 'visits': first, 'aggregate': aggregate, 'sessions': sessions}


def main():
    p = argparse.ArgumentParser()
    p.add_argument('phase', choices=['database-ready', 'configuration', 'database-down', 'replicas', 'failure', 'traffic', 'crash-enqueue', 'crash-verify'])
    p.add_argument('--pods', default='')
    p.add_argument('--seconds', type=int, default=50)
    args = p.parse_args()
    if args.phase == 'database-ready':
        deadline = time.monotonic() + 60
        while True:
            try:
                assert number('select 1') == 1
                break
            except (urllib.error.URLError, TimeoutError, ConnectionError):
                if time.monotonic() >= deadline:
                    raise
                time.sleep(.25)
        result = {'database_service_reachable': True}
    elif args.phase == 'configuration':
        with urllib.request.urlopen(BASE + '/status', timeout=5) as response:
            assert response.status == 200
        try:
            urllib.request.urlopen(BASE + '/live', timeout=5)
            raise AssertionError('removed /live route still exists')
        except urllib.error.HTTPError as e:
            assert e.code == 404, e.code
        assert number("select count(*) from sqlite_schema where type='table' and name='store'") == 0
        result = {'health_endpoint': '/status', 'removed_live_status': 404, 'obsolete_store_table': False}
    elif args.phase == 'database-down':
        checks = 0
        end = time.monotonic() + 35
        while time.monotonic() < end:
            for addr in args.pods.split(','):
                try:
                    urllib.request.urlopen('http://' + addr + ':8080/status', timeout=6)
                    raise AssertionError('replica reported ready without the database')
                except urllib.error.HTTPError as e:
                    assert e.code == 503, e.code
                checks += 1
            time.sleep(1)
        result = {'readiness_failures': checks, 'outage_seconds': 35}
    elif args.phase == 'replicas':
        targets = args.pods.split(',')
        for addr in targets:
            for _ in range(3):
                assert hit('http://' + addr + ':8080', '/replicas/same') == 200
                time.sleep(.4)
        result = verify('replicas', 3 * len(targets), 1)
        result['replicas_contacted'] = len(targets)
    elif args.phase == 'failure':
        sql("create trigger injected_failure before insert on hits begin select raise(abort,'cluster retry test'); end")
        try:
            for i in range(12):
                assert hit(BASE, '/failure/' + str(i)) == 200
            time.sleep(3)
            assert number('select count(*) from hit_queue') == 12
            assert number("select count(*) from paths where path like '/failure/%'") == 0
        finally:
            sql('drop trigger injected_failure')
        result = verify('failure', 12, 12)
        result['rolled_back_and_retried'] = True
    elif args.phase == 'traffic':
        accepted, failures = 0, []
        end = time.monotonic() + args.seconds
        while time.monotonic() < end:
            status = hit(BASE, '/rolling/' + str(accepted))
            if status == 200:
                accepted += 1
            else:
                failures.append(status)
            time.sleep(.08)
        assert not failures, ('HTTP failures during rolling update', failures)
        result = verify('rolling', accepted, accepted)
        result['http_failures'] = len(failures)
    elif args.phase == 'crash-enqueue':
        for i in range(24):
            assert hit(BASE, '/crash/' + str(i)) == 200
        assert number('select count(*) from hit_queue') == 24
        assert number("select count(*) from paths where path like '/crash/%'") == 0
        result = {'accepted_before_kill': 24, 'durable_pending': 24}
    else:
        result = verify('crash', 24, 24)
        # All phases use the same client; replacements must preserve its identity.
        assert number('select count(distinct session) from hits') == 1
        result['session_survived_replacement'] = True
    print(json.dumps({'phase': args.phase, **result}), flush=True)


if __name__ == '__main__':
    main()
