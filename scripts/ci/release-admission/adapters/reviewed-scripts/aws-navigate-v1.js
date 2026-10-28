async (page,input) => {
  const allowed=["https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home", "https://eu-central-1.console.aws.amazon.com/secretsmanager/listsecrets?region=eu-central-1", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/home", "https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create", "https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies"];
  const fail=()=>{throw new Error('chrome-public-navigation:refused');};
  if (!allowed.includes(input.url)) fail();
  const cdp=await page.context().newCDPSession(page);
  try {
    const identity=async()=>{
      const { targetInfo: t } = await cdp.send('Target.getTargetInfo');
      const { frameTree: tree } = await cdp.send('Page.getFrameTree');
      const f = tree.frame;
      if (f.parentId || t.type !== 'page' || !t.browserContextId || !f.loaderId) fail();
      const result = {targetId:t.targetId, targetType:t.type, targetURL:t.url,
        browserContextId:t.browserContextId, mainFrameId:f.id, loaderId:f.loaderId, frameURL:f.url,
        frameURLRelation:f.url === t.url ? 'same-url' :
          f.url === t.url.split('#', 1)[0] && t.url.includes('#') ? 'target-url-without-fragment' : 'unrelated'};
      return result;
    };
    const before=await identity();
    if (Object.keys(input.expected).length!==8 || Object.keys(before).some(k=>before[k]!==input.expected[k])) fail();
    if (!['about:blank','https://example.com/',...allowed].includes(before.targetURL)) fail();
    await page.goto(input.url,{waitUntil:'domcontentloaded',timeout:5000});
    const after=await identity();
    if (after.targetURL!==input.url || after.targetId!==before.targetId ||
        after.browserContextId!==before.browserContextId || after.mainFrameId!==before.mainFrameId ||
        !['same-url','target-url-without-fragment'].includes(after.frameURLRelation)) fail();
    return {schema:'hikyo-158/chrome-public-navigation/v1',before,after,observedAtMS:Date.now(),
      controlLeaseClaimed:false,exclusiveControlVerifiedByAdapter:false};
  } catch { fail(); } finally { await cdp.detach(); }
}
