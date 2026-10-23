async (page, input) => {
  const expected = input.expected;
  const kind = "iam";
  const fail = () => { throw new Error('chrome-public-readout:identity-or-public-custody'); };
  if (!["https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/roles", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/identity_providers", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=permissions", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=security_credentials", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=groups", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=tags", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn:aws:iam::099256073061:policy/hikyo-158-role-boundary", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn:aws:iam::099256073061:policy/hikyo-158-role-boundary?section=permissions", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn:aws:iam::099256073061:policy/hikyo-158-role-boundary?section=versions", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn:aws:iam::099256073061:policy/hikyo-158-role-boundary?section=entities", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn:aws:iam::099256073061:policy/hikyo-158-role-boundary?section=tags"].includes(expected.targetURL)) fail();
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
      const slots=["user.GetUser", "user.ListMFADevices", "user.ListAttachedUserPolicies", "user.ListUserPolicies", "user.ListGroupsForUser", "user.ListAccessKeys", "roles.ListRoles", "boundary.GetPolicy", "provider.ListOpenIDConnectProviders"];
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
