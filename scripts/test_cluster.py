#!/usr/bin/env python3
"""Verify replicas, rollback/retry, rolling updates and abrupt pod loss on kind.

Requires a disposable kind cluster, kubectl, Docker, and a built local image.
Creates only a labeled verification namespace; never changes the current context.
"""
import argparse
import json
import pathlib
import subprocess
import time
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--kubeconfig', required=True)
p.add_argument('--kind', default='kind')
p.add_argument('--cluster', default='goatcounter-test')
p.add_argument('--namespace', default='goatcounter-proof')
p.add_argument('--image', default='goatcounter-goatcounter:latest')
p.add_argument('--reset', action='store_true', help='replace this script\'s existing verification namespace')
a = p.parse_args()
K = ['kubectl', '--kubeconfig', a.kubeconfig, '-n', a.namespace]


def run(args, data=None):
    done = subprocess.run(args, input=data, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if done.returncode:
        raise RuntimeError(' '.join(args) + '\n' + done.stderr + done.stdout)
    return done.stdout


def k(*args, data=None):
    return run(K + list(args), data)


def apply(obj):
    return k('apply', '-f', '-', data=json.dumps(obj))


def rollout():
    print(k('rollout', 'status', 'deployment/goatcounter', '--timeout=180s').strip(), flush=True)


def probe(phase, *args):
    print('Checking ' + phase, flush=True)
    output = k('exec', 'probe', '--', 'python', '/tests/cluster_probe.py', phase, *args)
    result = json.loads(output.strip().splitlines()[-1])
    report['checks'].append(result)
    print(json.dumps(result), flush=True)


def app_pods():
    return [pod for pod in json.loads(k('get', 'pods', '-l', 'app=goatcounter', '-o', 'json'))['items']
            if not pod['metadata'].get('deletionTimestamp')]


report = {'checks': [], 'namespace': a.namespace, 'cluster': a.cluster}
existing = k('get', 'namespace', a.namespace, '--ignore-not-found', '-o', 'json')
if existing:
    owned = json.loads(existing).get('metadata', {}).get('labels', {}).get('goatcounter-verification') == 'true'
    if not a.reset or not owned:
        raise SystemExit('Namespace exists; use --reset only for a namespace created by this script.')
    k('delete', 'namespace', a.namespace, '--wait=true', '--timeout=120s')
apply({'apiVersion':'v1','kind':'Namespace','metadata':{'name':a.namespace,'labels':{'goatcounter-verification':'true'}}})
run(['docker','tag',a.image,'goatcounter:cluster-test'])
report['image_id'] = run(['docker','image','inspect',a.image,'--format','{{.Id}}']).strip()
run(['docker','pull','python:3.13-alpine'])
# Export only the host platform. Docker Desktop may retain a multi-platform
# index whose other manifests/attestations were not pulled; kind cannot import
# those missing objects with its default all-platform image loader.
platform = run(['docker','image','inspect',a.image,'--format','{{.Os}}/{{.Architecture}}']).strip()
with tempfile.TemporaryDirectory(prefix='goatcounter-images-') as tmp:
    archive = str(pathlib.Path(tmp)/'images.tar')
    run(['docker','image','save','--platform',platform,'-o',archive,'goatcounter:cluster-test','python:3.13-alpine'])
    run([a.kind,'load','image-archive',archive,'--name',a.cluster])
k('apply','-f',str(ROOT/'deploy/kubernetes/test-database.yaml'))
k('rollout','status','statefulset/libsql','--timeout=180s')
# Check the backing service through its network route as well as pod readiness
# before asserting that initial app starts need no retries.
k('wait','--for=condition=Ready','pod/libsql-0','--timeout=120s')
apply({'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':'verification-code'},'data':{
    'cluster_probe.py':(ROOT/'scripts/cluster_probe.py').read_text()}})
apply({'apiVersion':'v1','kind':'Pod','metadata':{'name':'probe'},'spec':{
    'containers':[{'name':'probe','image':'python:3.13-alpine','command':['sleep','3600'],
        'volumeMounts':[{'name':'tests','mountPath':'/tests'}]}],
    'volumes':[{'name':'tests','configMap':{'name':'verification-code'}}]}})
k('wait','--for=condition=Ready','pod/probe','--timeout=120s')
probe('database-ready')
apply({'apiVersion':'v1','kind':'Secret','metadata':{'name':'goatcounter-config'},'stringData':{
    'GOATCOUNTER_DB':'http://libsql:8080','GOATCOUNTER_RATELIMIT':'count:none'}})
k('apply','-f',str(ROOT/'deploy/kubernetes/app.yaml'))
rollout()
pods = app_pods()
report['initial_pods'] = [{'name':p['metadata']['name'],'node':p['spec']['nodeName'],
    'restarts':sum(c['restartCount'] for c in p['status']['containerStatuses'])} for p in pods]
assert len(pods) == 3, 'expected three active app replicas'
assert all(p['restarts'] == 0 for p in report['initial_pods']), report['initial_pods']
assert len({p['node'] for p in report['initial_pods']}) >= 2, 'replicas must span nodes'
for pod in pods:
    logs = [json.loads(line) for line in k('logs', pod['metadata']['name']).splitlines() if line.strip()]
    assert any(item.get('msg') == 'startup: GoatCounter ready' for item in logs), logs
report['default_json_logs'] = True
probe('configuration')
# Readiness must withdraw traffic during an outage; liveness must not turn a
# shared database failure into a restart of every application replica.
k('scale', 'statefulset/libsql', '--replicas=0')
try:
    k('wait', '--for=delete', 'pod/libsql-0', '--timeout=60s')
    k('wait', '--for=condition=Ready=false', 'pods', '-l', 'app=goatcounter', '--timeout=30s')
    probe('database-down', '--pods', ','.join(p['status']['podIP'] for p in pods))
    current = app_pods()
    assert {p['metadata']['uid'] for p in current} == {p['metadata']['uid'] for p in pods}
    assert all(c['restartCount'] == 0 for p in current for c in p['status']['containerStatuses'])
    report['checks'][-1]['app_restarts'] = 0
finally:
    k('scale', 'statefulset/libsql', '--replicas=1')
    k('rollout', 'status', 'statefulset/libsql', '--timeout=120s')
    k('wait', '--for=condition=Ready', 'pods', '-l', 'app=goatcounter', '--timeout=60s')
probe('replicas','--pods',','.join(p['status']['podIP'] for p in pods))
probe('failure')
print('Sending service traffic while Kubernetes replaces the Deployment',flush=True)
traffic = subprocess.Popen(K+['exec','probe','--','python','/tests/cluster_probe.py','traffic','--seconds','50'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
time.sleep(2)
old_names = {p['metadata']['name'] for p in app_pods()}
start = time.monotonic()
k('rollout','restart','deployment/goatcounter')
rollout()
report['rolling_update_seconds'] = round(time.monotonic()-start,2)
assert not old_names & {p['metadata']['name'] for p in app_pods()}, 'old pods still running'
stdout,stderr = traffic.communicate(timeout=100)
assert traffic.returncode == 0, stdout+stderr
result = json.loads(stdout.strip().splitlines()[-1]); report['checks'].append(result)
print(json.dumps(result),flush=True)
# Freeze workers long enough to kill every application pod with queued work.
k('set','env','deployment/goatcounter','GOATCOUNTER_STORE_EVERY=600')
rollout()
probe('crash-enqueue')
old_names = {p['metadata']['name'] for p in app_pods()}
k('delete','pods','-l','app=goatcounter','--grace-period=0','--force','--wait=true')
k('set','env','deployment/goatcounter','GOATCOUNTER_STORE_EVERY=1')
rollout()
assert not old_names & {p['metadata']['name'] for p in app_pods()}
probe('crash-verify')
report['final_pods'] = [{'name':p['metadata']['name'],'node':p['spec']['nodeName'],
    'ready':all(c['ready'] for c in p['status']['containerStatuses'])} for p in app_pods()]
report['passed'] = True
out = ROOT/'.test-results';out.mkdir(exist_ok=True)
(out/'cluster-report.json').write_text(json.dumps(report,indent=2)+'\n')
print('PASS: '+str(out/'cluster-report.json'),flush=True)
