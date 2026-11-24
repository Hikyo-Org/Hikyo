import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { parseDocument } from 'yaml';
import { z } from 'zod';

const workflowSchema = z.object({
  jobs: z.object({
    'fallback-channel': z.object({
      permissions: z.object({ issues: z.literal('write') }),
      steps: z.array(z.object({
        name: z.string().optional(),
        if: z.string().optional(),
        run: z.string().optional(),
      })),
    }),
  }),
});

// Approved notification body from a786ff5668b20d1488f7a8bf1b4f71a62a50708d
// (#1410), .github/workflows/reliability.yml, which binds the alert lookup to the
// workflow bot's numeric id. Pin executable behavior, not shell-looking
// comment/string decoys. Intentional notification changes require a reviewed
// update to this locked OSS contract.
const approvedNotificationDigest = 'c8467744a67573d92b50b5a35bae75ca4b612ec0983d31d66641862067963da0';

try {
  if (process.argv.length !== 3) throw new Error('expected one fallback workflow path');
  const document = parseDocument(readFileSync(process.argv[2], 'utf8'), { uniqueKeys: true });
  if (document.errors.length) throw new Error(document.errors.map((error) => error.message).join('; '));
  const workflow = workflowSchema.parse(document.toJS());
  const alerts = workflow.jobs['fallback-channel'].steps.filter(
    (step) => step.name === 'Surface fallback-channel degradation',
  );
  if (alerts.length !== 1) throw new Error('require exactly one fallback degradation step');
  const [alert] = alerts;
  if (alert.if !== 'failure()' ||
      createHash('sha256').update(alert.run ?? '').digest('hex') !== approvedNotificationDigest) {
    throw new Error('fallback degradation step must run the approved failure notification body');
  }
} catch (error) {
  console.error(`OSS policy gate: fallback-channel workflow: ${error.message}`);
  process.exit(1);
}
