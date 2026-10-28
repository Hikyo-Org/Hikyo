"""Inert Chrome public-readout custody. No import I/O, transport or authority.

SQL equality is necessary custody, not Root authentication. Root retains the
whole original and authenticates it before writing the holder release pipe.
The installed CLI's automatic response capture remains a runtime blocker.
"""
import copy
import hashlib
import re
import shlex
import time
from urllib.parse import parse_qsl, urlsplit


CLI_EXECUTABLE_SHA256 = '66ea6722d77e57ce1bc7e850cb990d14895d17d8584405ed54a8c72fab38eb75'
CLI_CORE_SOURCE_SHA256 = 'adf9246486a03bee08b2895db057ba470c62377911d291693fe783d91d6fec52'
THREAD = '521da58e-e4b9-4be1-81ad-50cf8093b41a'
IDENTITY_KEYS = {'targetId', 'targetType', 'targetURL', 'browserContextId',
                 'mainFrameId', 'loaderId', 'frameURL', 'frameURLRelation'}
COMMAND_KEYS = {'schema', 'threadId', 'identity', 'session', 'executablePath',
                'executableSHA256', 'cliSourceSHA256', 'templateSHA256',
                'script', 'scriptSHA256', 'argv', 'command', 'kind',
                'controlGenerationSupported', 'externalRootExclusiveControlRequired',
                'externalRootAuthenticationRequired', 'transportReadBoundaryEstablished'}

READOUT_TEMPLATE = r'''async (page) => {
  const expected = __IDENTITY__;
  const kind = __KIND__;
  const fail = () => { throw new Error('chrome-public-readout:identity-or-public-custody'); };
  const cdp = await page.context().newCDPSession(page);
  try {
    const identity = async () => {
      const { targetInfo: t } = await cdp.send('Target.getTargetInfo');
      const { frameTree: tree } = await cdp.send('Page.getFrameTree');
      const f = tree.frame;
      if (f.parentId || t.type !== 'page' || !t.browserContextId || !f.loaderId) fail();
      const result = {targetId:t.targetId, targetType:t.type, targetURL:t.url,
        browserContextId:t.browserContextId, mainFrameId:f.id, loaderId:f.loaderId, frameURL:f.url,
        frameURLRelation:f.url === t.url ? 'same-url' :
          f.url === t.url.split('#', 1)[0] && t.url.includes('#') ? 'target-url-without-fragment' : 'unrelated'};
      if (Object.keys(expected).length !== 8 || Object.keys(result).some(k => result[k] !== expected[k])) fail();
      return result;
    };
    const before = await identity();
    const captured = await page.evaluate((mode) => {
      let value;
      if (mode === 'create') {
        value = {create:globalThis.__hikyo158NextKeyCustodyObserverV1.read('create'),
          guard:globalThis.__hikyo158NativeKMSXHRWriteGuardV1.status()};
      } else if (mode === 'historical') {
        value = globalThis.__hikyo158HistoricalDescribeObserverV2.read();
      } else if (mode === 'identity') {
        value = null;
      } else { throw new Error('chrome-public-readout:closed-mode'); }
      return {readAtMS:Date.now(), value};
    }, kind);
    const after = await identity();
    return {schema:'hikyo-158/chrome-public-readout/v1', kind, before, after,
      readAtMS:captured.readAtMS, value:captured.value,
      controlLeaseClaimed:false, exclusiveControlVerifiedByAdapter:false};
  } finally { await cdp.detach(); }
}'''


def raw_sha(raw):
    return hashlib.sha256(raw).hexdigest()


def identity_checked(m, identity):
    m.exact(identity, IDENTITY_KEYS)
    m.require(all(type(identity[k]) is str and 1 <= len(identity[k]) <= 256
                  and re.fullmatch(r'[A-Za-z0-9._:-]+', identity[k])
                  for k in ('targetId', 'browserContextId', 'mainFrameId', 'loaderId'))
              and identity['targetType'] == 'page'
              and type(identity['targetURL']) is str
              and ((identity['frameURLRelation']=='same-url' and identity['frameURL']==identity['targetURL'])
                  or (identity['frameURLRelation']=='target-url-without-fragment'
                      and '#' in identity['targetURL'] and identity['frameURL']==identity['targetURL'].split('#',1)[0]))
              and type(identity['frameURL']) is str and len(identity['frameURL']) <= 2048
              and (identity['frameURL']=='https://example.com/' or re.fullmatch(
                  r'https://(?:[a-z0-9-]+\.)*console\.aws\.amazon\.com/[A-Za-z0-9_./?=&%#:+,-]*', identity['frameURL'])),
              'chrome-explicit-CDP-page-target-context-frame-loader-public-route')
    return identity


def readout_script(m, identity, kind):
    identity_checked(m, identity)
    m.require(kind in ('create', 'historical', 'identity'), 'chrome-closed-public-readout-kind')
    url = identity['targetURL']
    parsed = urlsplit(url)
    parameters = parse_qsl(parsed.query, keep_blank_values=True)
    historical = (parsed.netloc == 'eu-central-1.console.aws.amazon.com' and not parsed.fragment
        and ((parsed.path in ('/secretsmanager/listsecrets', '/secretsmanager/home')
              and parameters == [('region', m.REGION)])
             or (parsed.path == '/secretsmanager/secret' and len(parameters) == 2
                 and sorted(parameters) == sorted([('name', m.HISTORICAL), ('region', m.REGION)]))))
    create = url == 'https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create'
    m.require((kind == 'create' and create) or (kind == 'historical' and historical)
              or (kind == 'identity' and (create or historical
                  or url == 'https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home'
                  or url == 'https://example.com/')), 'chrome-closed-public-route-for-readout-kind')
    return READOUT_TEMPLATE.replace('__IDENTITY__', m.canonical(identity).decode()).replace('__KIND__', m.canonical(kind).decode())


def invocation(m, *, identity, kind, session, executable_path, executable_sha256, cli_source_sha256):
    """Return exact inert argv/command. Never spawn, discover or read files."""
    script = readout_script(m, identity, kind)
    value = {'schema': 'hikyo-158/chrome-owner-command-context/v1', 'threadId': THREAD,
             'identity': copy.deepcopy(identity), 'kind': kind, 'session': session,
             'executablePath': executable_path, 'executableSHA256': executable_sha256,
             'cliSourceSHA256': cli_source_sha256, 'templateSHA256': raw_sha(READOUT_TEMPLATE.encode()),
             'script': script, 'scriptSHA256': raw_sha(script.encode()),
             'argv': [executable_path, '-s=' + session, '--json', '--raw', 'run-code', script],
             'controlGenerationSupported': False, 'externalRootExclusiveControlRequired': True,
             'externalRootAuthenticationRequired': True, 'transportReadBoundaryEstablished': False}
    value['command'] = shlex.join(value['argv'])
    command_context_checked(m, value)
    return value


def command_context_checked(m, value):
    m.exact(value, COMMAND_KEYS)
    identity_checked(m, value['identity'])
    m.require(value['schema'] == 'hikyo-158/chrome-owner-command-context/v1'
              and value['threadId'] == THREAD
              and type(value['session']) is str and re.fullmatch(r'[A-Za-z0-9_-]{8,100}', value['session'])
              and type(value['executablePath']) is str and value['executablePath'].startswith('/')
              and '\n' not in value['executablePath']
              and all(m.sha(value[k]) for k in ('executableSHA256', 'cliSourceSHA256', 'templateSHA256', 'scriptSHA256'))
              and value['executableSHA256'] == CLI_EXECUTABLE_SHA256
              and value['cliSourceSHA256'] == CLI_CORE_SOURCE_SHA256
              and value['templateSHA256'] == raw_sha(READOUT_TEMPLATE.encode())
              and value['script'] == readout_script(m, value['identity'], value['kind'])
              and value['scriptSHA256'] == raw_sha(value['script'].encode())
              and value['argv'] == [value['executablePath'], '-s=' + value['session'], '--json', '--raw', 'run-code', value['script']]
              and value['command'] == shlex.join(value['argv'])
              and value['controlGenerationSupported'] is False
              and value['externalRootExclusiveControlRequired'] is True
              and value['externalRootAuthenticationRequired'] is True
              and value['transportReadBoundaryEstablished'] is False,
              'chrome-exact-reviewed-source-command-script-session-no-lease-claim')


def verify_native_original(connection, raw, model, sequence_min, sequence_max):
    """Copied unchanged from adopted root_prebind_banked verification function."""
    model.require(type(sequence_min) is int and type(sequence_max) is int
                  and 0 <= sequence_min <= sequence_max <= sequence_min + 50000,
                  'prebind-bounded-native-sequence-window')
    original = model.strict_json(raw)
    model.require(original['type'] == 'command_execution' and original['status'] == 'completed'
                  and type(original['exitCode']) is int and original['exitCode'] == 0
                  and type(original['output']) is str, 'prebind-complete-native-command')
    model.require(model.instant(original['startedAt']) <= model.instant(original['completedAt'])
                  <= model.instant(original['updatedAt']), 'prebind-original-command-chronology')
    native_id = original['nativeItemRef']['nativeId']
    model.require(type(native_id) is str and 8 <= len(native_id) <= 256, 'prebind-native-id')
    deadline = time.monotonic() + 5
    connection.set_progress_handler(lambda: int(time.monotonic() > deadline), 2000)
    try:
        rows = connection.execute(
            "SELECT sequence,payload_json FROM orchestration_events WHERE sequence>=? AND sequence<=? "
            "AND stream_id=? AND event_type='turn-item.updated' AND payload_json=? ORDER BY sequence LIMIT 2",
            (sequence_min, sequence_max, THREAD, raw.decode('utf-8'))).fetchall()
    finally:
        connection.set_progress_handler(None, 0)
    matches = [sequence for sequence, payload in rows if payload.encode('utf-8') == raw]
    model.require(len(matches) == 1, 'prebind-exact-one-genuine-native-payload')
    return original, matches[0]


def command_readout_checked(m, raw, command, *, connection, sequence_min, sequence_max, explicit_now):
    """Check whole genuine native bytes, then strict installed --json result.

    No rewritten native row is produced. Whole raw and parsed hashes differ
    intentionally. Caller supplies a bounded read-only current-stream connection.
    """
    command_context_checked(m, command)
    m.require(type(raw) is bytes and 0 < len(raw) <= 1_999_999, 'chrome-bounded-whole-native-original')
    original, sequence = verify_native_original(connection, raw, m, sequence_min, sequence_max)
    m.require(original['input'] == command['command']
              and ('threadId' not in original or original['threadId'] == THREAD), 'chrome-exact-native-command-input')
    m.clock(explicit_now)
    m.fresh(original['completedAt'], explicit_now)
    m.require(m.instant(original['updatedAt']) <= explicit_now, 'chrome-completed-current-original')
    # Installed Response.serialize: Result is joined text; runCode JSON.stringify
    # serializes the returned object into that text. --raw is ONLY output filtering.
    envelope = m.strict_json(original['output'].encode())
    m.exact(envelope, {'result'})
    m.require(type(envelope['result']) is str, 'chrome-installed-result-is-serialized-string')
    result = m.strict_json(envelope['result'].encode())
    m.exact(result, {'schema', 'kind', 'before', 'after', 'readAtMS', 'value',
                     'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter'})
    identity_checked(m, result['before']); identity_checked(m, result['after'])
    m.require(result['schema'] == 'hikyo-158/chrome-public-readout/v1'
              and result['kind'] == command['kind']
              and m.same(result['before'], command['identity']) and m.same(result['after'], command['identity'])
              and type(result['readAtMS']) is int and result['readAtMS'] >= 0
              and result['controlLeaseClaimed'] is False and result['exclusiveControlVerifiedByAdapter'] is False
              and m.instant(original['startedAt']).timestamp() * 1000 <= result['readAtMS']
              <= m.instant(original['completedAt']).timestamp() * 1000,
              'chrome-same-actual-target-frame-loader-around-public-readout')
    return {'original': original, 'originalRawSHA256': raw_sha(raw), 'originalSHA256': m.digest(original),
            'originalSequence': sequence, 'commandContextSHA256': m.digest(command), 'readout': result}
