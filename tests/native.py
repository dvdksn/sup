#!/usr/bin/env python3
"""Exercise the compiled CLI across process boundaries with native SBX/Herdr doubles."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SUP = str(Path(os.environ.get("SUP", "bin/sup")).resolve())
FAKE = r'''#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
args=sys.argv[1:]; program=pathlib.Path(sys.argv[0]).name
root=pathlib.Path(os.environ['SUP_NATIVE_ROOT']); db=root/'db.json'
state=json.loads(db.read_text()) if db.exists() else {'sandboxes': [], 'machines': [], 'workspaces': [], 'next': 0}
with (root/'log.jsonl').open('a') as f: f.write(json.dumps([program]+args)+'\n')
def save(): db.write_text(json.dumps(state))
def value(flag): return args[args.index(flag)+1]
if program=='sbx':
 if args==['ls','--json']:
  if os.environ.get('FAIL_INVENTORY'): sys.exit(13)
  print(json.dumps({'sandboxes': state['sandboxes']})); sys.exit()
 if args[:2]==['env','plan']: print('Native plan'); sys.exit()
 if args[:2]==['env','run']:
  name=value('--name'); found=next((s for s in state['sandboxes'] if s['name']==name),None)
  if not found:
   state['next']+=1
   state['sandboxes'].append({'name':name,'id':'sandbox-'+str(state['next']),'status':'running'})
   save()
   files=[arg for arg in args[2:] if arg.startswith('/') and pathlib.Path(arg).is_file()]
   overlay=json.loads(pathlib.Path(files[-1]).read_text())
   for hook in overlay.get('lifecycle',{}).get('postCreate',[]):
    result=subprocess.run(hook['command'],shell=True)
    if result.returncode: sys.exit(result.returncode)
  else: found['status']='running'; save()
  sys.exit()
 if args[:2]==['env','exec']:
  assert args[2]=='--name', 'env exec requires flags before file operands'
  next(s for s in state['sandboxes'] if s['name']==value('--name'))['status']='running';save();sys.exit()
 if args[:2]==['env','rm']:
  if os.environ.get('CANCEL_REMOVE'): print('Aborted.',file=sys.stderr); sys.exit()
  state['sandboxes']=[s for s in state['sandboxes'] if s['name']!=value('--name')]; save(); sys.exit()
 if args[0]=='stop':
  next(s for s in state['sandboxes'] if s['name']==args[1])['status']='stopped'; save(); sys.exit()
 if args[0]=='mount' and os.environ.get('FAIL_HISTORY'): sys.exit(17)
 if args[0] in ['mount','exec','setup']: sys.exit()
if program=='herdr':
 if args==['machine','list','--json']: print(json.dumps(state['machines'])); sys.exit()
 if args[:2]==['machine','add']:
  state['machines'].append({'id':'machine-'+str(state['next']),'target':args[2],'session':value('--remote-session'),'enabled':True}); save(); sys.exit()
 if args[:2] in [['machine','enable'],['machine','disable']]:
  next(m for m in state['machines'] if m['id']==args[2])['enabled']=args[1]=='enable';save();sys.exit()
 if args[:2]==['machine','remove']:
  state['machines']=[m for m in state['machines'] if m['id']!=args[2]];state['workspaces']=[];save();sys.exit()
 if args[0]=='--machine':
  if args[2:]==['workspace','list']: result={'workspaces':state['workspaces']}
  elif args[2:4]==['workspace','create']:
   w={'workspace_id':'w'+str(len(state['workspaces'])+1),'label':value('--label')};state['workspaces'].append(w);save();result={'workspace':w}
  elif args[2:]==['server','reload-config']: result={}
  else: sys.exit(2)
  print(json.dumps({'result':result}));sys.exit()
if program=='ssh': sys.exit()
print('unexpected command '+repr([program]+args),file=sys.stderr);sys.exit(2)
'''

class NativeProjects(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix="sup space '")
  self.addCleanup(self.tmp.cleanup)
  self.root=Path(self.tmp.name)
  bin=self.root/'bin';bin.mkdir()
  for name in ['sbx','herdr','ssh']:
   path=bin/name;path.write_text(FAKE);path.chmod(0o700)
  self.env=os.environ.copy()
  self.env.update(XDG_CONFIG_HOME=str(self.root/'config'), XDG_STATE_HOME=str(self.root/'state'), XDG_DATA_HOME=str(self.root/'data'), SUP_NATIVE_ROOT=str(self.root), PATH=str(bin)+os.pathsep+os.environ['PATH'], HERDR_BIN_PATH=str(bin/'herdr'))
  self.envfile=self.root/'custom environment.yaml'
  self.envfile.write_text('schemaVersion: "1"\nname: custom\nagent: kit-shell\nargs:\n  repo: {required: true}\n')
 def run_sup(self,*args,code=0):
  p=subprocess.run([SUP,*args],env=self.env,text=True,capture_output=True)
  self.assertEqual(p.returncode,code,(args,p.stdout,p.stderr))
  return p
 def start(self,*args): return self.run_sup('docker/docs','--native','--env-file',str(self.envfile),*args)
 def record(self): return json.loads((self.root/'state/sup/docker-docs/project.json').read_text())
 def calls(self): return [json.loads(l) for l in (self.root/'log.jsonl').read_text().splitlines()]
 def db(self): return json.loads((self.root/'db.json').read_text())
 def write_db(self,value): (self.root/'db.json').write_text(json.dumps(value))

 def test_recreation_keeps_history_and_configuration(self):
  self.start('-d','--env-arg','model=x = y','--kit','registry/extra:latest')
  p=self.record(); history=Path(p['history']);(history/'conversation.jsonl').write_text('keep')
  original_id=p['sandboxId']
  self.run_sup('stop','docker-docs')
  self.run_sup('docker-docs','--agent','claude')
  self.assertEqual(self.record()['sandboxId'],original_id)
  self.assertTrue(any(c[:3]==['sbx','env','exec'] for c in self.calls()))
  self.run_sup('rm','docker-docs','--force')
  self.assertEqual((history/'conversation.jsonl').read_text(),'keep')
  self.assertTrue(self.record()['removed'])
  self.run_sup('docker-docs','-d')
  self.assertNotEqual(self.record()['sandboxId'],original_id)
  self.run_sup('recreate','docker-docs','--force','-d')
  self.assertEqual((history/'conversation.jsonl').read_text(),'keep')
  self.assertEqual(self.record()['args']['model'],'x = y')
  calls=self.calls()
  self.assertTrue(any('model=x = y' in c for c in calls))
  self.assertTrue(any(c==['sbx','exec','-it','--workdir','/home/agent/workspace','docker-docs','bash','-lc',"cd '/home/agent/workspace' && exec claude"] for c in calls))
  overlay=json.loads((self.root/'state/sup/docker-docs/project.sbxenv.yaml').read_text())
  self.assertEqual(set(overlay),{'schemaVersion','name','kits','env','lifecycle'})
  self.run_sup('history','clear','docker-docs','--yes',code=1)
  self.assertTrue((history/'conversation.jsonl').exists())
  self.run_sup('stop','docker-docs')
  self.run_sup('history','clear','docker-docs','--yes')
  self.assertFalse((history/'conversation.jsonl').exists())
  self.assertTrue(history.is_dir())
  self.assertEqual(self.run_sup('history','path','docker-docs').stdout.strip(),str(history))


 def test_history_uses_native_mount_without_an_installed_helper(self):
  self.start('-d')
  history=Path(self.record()['history'])
  self.assertTrue(any(c==['sbx','mount','docker-docs',str(history)+':/home/agent/project-history:rw'] for c in self.calls()))
  setup=[c for c in self.calls() if c[:5]==['sbx','exec','docker-docs','python3','-c']]
  self.assertTrue(setup)
  self.assertIn('def connect(',setup[0][5])
  self.assertEqual(setup[0][-2:],['docker-docs','/home/agent/project-history'])
  self.assertFalse(any('/usr/local/bin/sup-history' in c for c in self.calls()))

 def test_history_failure_blocks_attachment_and_recovers(self):
  self.env['FAIL_HISTORY']='1'
  self.run_sup('docker/docs','--native','--env-file',str(self.envfile),code=17)
  self.assertFalse(any('-it' in c for c in self.calls()))
  self.assertTrue(self.record()['sandboxId'])
  del self.env['FAIL_HISTORY']
  self.run_sup('docker-docs')
  self.assertTrue(any('-it' in c for c in self.calls()))

 def test_declined_removal_never_recreates(self):
  self.start('-d');p=self.record();count=len(self.calls())
  self.env['CANCEL_REMOVE']='1'
  self.run_sup('recreate','docker-docs','-d',code=1)
  self.assertEqual(self.record()['sandboxId'],p['sandboxId'])
  self.assertFalse(any(c[:3]==['sbx','env','run'] for c in self.calls()[count:]))

 def test_identity_mismatch_and_missing_are_not_silently_replaced(self):
  self.start('-d');db=self.db();db['sandboxes'][0]['id']='unrelated';self.write_db(db)
  self.run_sup('rm','docker-docs','--force',code=1)
  self.run_sup('docker-docs',code=1)
  db['sandboxes']=[];self.write_db(db)
  self.run_sup('docker-docs',code=1)
  self.run_sup('recreate','docker-docs','--force','-d')

 def test_environment_edits_require_deliberate_recreation(self):
  self.start('-d');self.envfile.write_text(self.envfile.read_text()+'env: {NEW: value}\n')
  self.run_sup('docker-docs','-d',code=1)
  self.run_sup('recreate','docker-docs','--force','-d')
  self.run_sup('docker-docs','-d')

 def test_plan_and_no_history_and_exit_codes(self):
  self.start('--plan')
  self.assertFalse((self.root/'state/sup/docker-docs/project.json').exists())
  self.start('--no-history','-d')
  self.assertNotIn('history',self.record())
  self.assertFalse(any(c[0:2]==['sbx','mount'] for c in self.calls()))
  self.run_sup('__complete','docker-',code=0)
  self.assertEqual(self.run_sup('ls','--json').returncode,0)
  self.env['FAIL_INVENTORY']='1';self.run_sup('docker-docs',code=13)

 def test_herdr_registers_once_and_recreates_remote_runtime(self):
  self.start('--via','herdr')
  self.run_sup('open','docker-docs','--via','herdr')
  self.assertEqual(len(self.db()['machines']),1)
  self.assertEqual(len(self.db()['workspaces']),1)
  self.run_sup('stop','docker-docs')
  self.assertFalse(self.db()['machines'][0]['enabled'])
  self.run_sup('docker-docs','--via','herdr')
  self.assertTrue(self.db()['machines'][0]['enabled'])
  self.env['CANCEL_REMOVE']='1'
  self.run_sup('rm','docker-docs',code=1)
  self.assertEqual(len(self.db()['machines']),1)
  self.assertTrue(self.db()['machines'][0]['enabled'])
  del self.env['CANCEL_REMOVE']
  self.run_sup('recreate','docker-docs','--force','--via','herdr')
  self.assertEqual(len(self.db()['machines']),1)
  self.assertEqual(len(self.db()['workspaces']),1)

 def test_default_environment_and_ssh(self):
  self.run_sup('docker/docs','--native','--no-history','--via','ssh')
  p=self.record();s=Path(p['files'][0]['path']).read_text()
  self.assertIn('kit-tools',s);self.assertIn('repo:',s)
  self.assertFalse(any(line.startswith('workspace:') for line in s.splitlines()))
  self.assertTrue(any(c[0]=='ssh' and c[2]=='docker-docs.sbx' for c in self.calls()))

if __name__=='__main__':unittest.main(verbosity=2)
