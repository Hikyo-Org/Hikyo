async (page, input) => {
  const expected = input.expected;
  const kind = "kms";
  const fail = () => { throw new Error('chrome-public-readout:identity-or-public-custody'); };
  if (!["https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/home", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6/keyPolicy", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6/cryptographicConfiguration", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6/keyRotation", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6/aliases", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6/tags", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6/keyMaterial", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205/keyPolicy", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205/cryptographicConfiguration", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205/keyRotation", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205/aliases", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205/tags", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205/keyMaterial", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e/keyPolicy", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e/cryptographicConfiguration", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e/keyRotation", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e/aliases", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e/tags", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e/keyMaterial", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1/keyPolicy", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1/cryptographicConfiguration", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1/keyRotation", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1/aliases", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1/tags", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1/keyMaterial"].includes(expected.targetURL)) fail();
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
    const captured = await page.evaluate((manifest) => {
      const observer=globalThis.__hikyo158NextInventoryPrearmObserverV1;
      const status=observer.status();
      const slots=["old1.DescribeKey", "old2.DescribeKey", "old3.DescribeKey", "old4.DescribeKey"];
      if (status.captureError || (status.pendingCaptures ?? status.pending) !== 0 ||
          status.readySlots.length !== slots.length || slots.some(slot => !status.readySlots.includes(slot)))
        throw new Error('chrome-public-metadata:incomplete');
      const value={manifest,readRows:slots.map(slot => observer.read(slot))};
      return {readAtMS:Date.now(),value};
    },input.manifest);
    const after = await identity();
    const payload=JSON.stringify(captured.value);
    if (payload.length>131072) fail();
    const chunks=[];
    for (let offset=0;offset<payload.length;) {
      let end=Math.min(offset+2048,payload.length);
      const last=payload.charCodeAt(end-1);
      if (end<payload.length && last>=0xD800 && last<=0xDBFF) end--;
      chunks.push(payload.slice(offset,end));offset=end;
    }
    if (chunks.length>64) fail();
    const publicWire={schema:'hikyo-158/public-json-chunks/v1',chunks};
    return {schema:'hikyo-158/chrome-public-readout/v1', kind, before, after,
      readAtMS:captured.readAtMS, value:publicWire,
      controlLeaseClaimed:false, exclusiveControlVerifiedByAdapter:false};
  } finally { await cdp.detach(); }
}
