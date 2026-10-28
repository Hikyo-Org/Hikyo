async (page, input) => {
  const expected = input.expected;
  const kind = "owner";
  const fail = () => { throw new Error('chrome-public-readout:identity-or-public-custody'); };
  if (!["https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home"].includes(expected.targetURL)) fail();
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
    const captured = await page.evaluate(() => {
      if (location.href !== 'https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home')
        throw new Error('chrome-public-owner:public-IAM-custody');
      const menu = document.querySelector('#nav-usernameMenu');
      const headings = Array.from(document.querySelectorAll('h1,h2,[role="heading"]'))
        .filter(node => node.textContent.trim() === 'IAM Dashboard');
      if (!menu || headings.length !== 1 || menu.textContent.trim() !== '099256073061')
        throw new Error('chrome-public-owner:public-IAM-custody');
      const readAtMS=Date.now();
      return {readAtMS,value:{schema:'hikyo-158/public-iam-owner-observation/v1',
        pageURL:location.href,accountHeaderText:menu.textContent.trim(),
        dashboardHeadingText:headings[0].textContent.trim(),capturedAt:new Date(readAtMS).toISOString()}};
    });
    const after = await identity();
    return {schema:'hikyo-158/chrome-public-readout/v1', kind, before, after,
      readAtMS:captured.readAtMS, value:captured.value,
      controlLeaseClaimed:false, exclusiveControlVerifiedByAdapter:false};
  } finally { await cdp.detach(); }
}
