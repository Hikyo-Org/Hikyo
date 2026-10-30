// Pass the transient bearer to Inspector's own CLI in process memory only.
// The OS argv and on-disk configuration never contain the header value.
import { pathToFileURL } from 'node:url';

const { HIKYO_INSPECTOR_ENTRY: entry, HIKYO_MCP_URL: endpoint,
  HIKYO_MCP_TOKEN: token, HIKYO_MCP_ORG_ID: org,
  HIKYO_MCP_PROJECT_ID: project, HIKYO_MCP_ENV_ID: environment,
  HIKYO_INSPECTOR_METHOD: method } = process.env;
if (!entry || !endpoint || !token || !org || !project || !environment ||
    !['initialize', 'tools/call'].includes(method)) {
  throw new Error('Protected Inspector inputs required');
}
const origin = new URL(endpoint).origin;
const fetchVerified = globalThis.fetch;
globalThis.fetch = async (input, init) => {
  const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
  if (url.protocol !== 'https:' || url.origin !== origin) {
    throw new Error('Inspector fetch outside the approved origin refused');
  }
  return fetchVerified(input, { ...init, redirect: 'manual' });
};
const { runCli } = await import(pathToFileURL(entry).href);
const args = [process.execPath, entry, '--server-url', endpoint,
  '--protocol-era', 'modern', '--header', `Authorization: Bearer ${token}`,
  '--method', method];
if (method === 'tools/call') {
  args.push('--tool-name', 'hikyo_inspect_configuration', '--tool-arg',
    `org_id=${org}`, `project_id=${project}`, `environment_id=${environment}`);
}
try {
  await runCli(args);
} catch {
  process.stderr.write('Pinned Inspector call failed; output withheld\n');
  process.exitCode = 1;
}
