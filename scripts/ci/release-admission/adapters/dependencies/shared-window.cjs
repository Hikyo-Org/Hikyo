'use strict';
// Actual browser clock only. No renewal, caller-supplied now, or replacement deadline.
function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
module.exports = { checkSharedWindow };
