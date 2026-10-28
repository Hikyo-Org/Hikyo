async (page, input) => {
  const expected = input.expected;
  const kind = "identity";
  const fail = () => { throw new Error('chrome-public-readout:identity-or-public-custody'); };
  if (!["https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home", "https://eu-central-1.console.aws.amazon.com/secretsmanager/listsecrets?region=eu-central-1", "https://eu-central-1.console.aws.amazon.com/secretsmanager/home?region=eu-central-1", "https://eu-central-1.console.aws.amazon.com/secretsmanager/secret?name=arn%3Aaws%3Asecretsmanager%3Aeu-central-1%3A099256073061%3Asecret%3Ahikyo-e2e-20261018b%2Fweb-identity%2Fpostgres%2Frole-document-vmN1Vj&region=eu-central-1", "https://eu-central-1.console.aws.amazon.com/secretsmanager/secret?region=eu-central-1&name=arn%3Aaws%3Asecretsmanager%3Aeu-central-1%3A099256073061%3Asecret%3Ahikyo-e2e-20261018b%2Fweb-identity%2Fpostgres%2Frole-document-vmN1Vj", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create"].includes(expected.targetURL)) fail();
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
}
