import { Input } from '../ui/Input.tsx';

/**
 * AWS Secrets Manager access (#158). The fields assemble the adapter's
 * write-only access descriptor, which is sealed as the credential and never
 * read back. Only the static secret access key is secret; the modes that use
 * the server's own AWS identity need the node operator's opt-in
 * (HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY=allow) and carry no secret at all.
 */
export type AwsAuthMode = 'ambient' | 'assume-role' | 'web-identity' | 'static';

export type AwsAccess = {
  readonly mode: AwsAuthMode;
  readonly roleArn: string;
  readonly externalId: string;
  readonly sessionSeconds: string;
  readonly region: string;
  readonly accessKeyId: string;
  readonly secretAccessKey: string;
};

export const emptyAwsAccess: AwsAccess = {
  mode: 'assume-role',
  roleArn: '',
  externalId: '',
  sessionSeconds: '',
  region: '',
  accessKeyId: '',
  secretAccessKey: '',
};

const modes: readonly { readonly value: AwsAuthMode; readonly label: string }[] = [
  { value: 'assume-role', label: 'Assume role (server identity as source)' },
  { value: 'web-identity', label: 'Web identity role (server token)' },
  { value: 'ambient', label: 'Server workload identity' },
  { value: 'static', label: 'Static access key' },
];

/** The descriptor fields that apply to a mode; anything else is omitted. */
export function awsAccessDescriptor(access: AwsAccess): string {
  const descriptor: Record<string, string | number> = { mode: access.mode };
  if (access.region.trim() !== '') descriptor.region = access.region.trim();
  if (access.mode === 'assume-role' || access.mode === 'web-identity') {
    descriptor.role_arn = access.roleArn.trim();
    if (access.mode === 'assume-role' && access.externalId.trim() !== '') {
      descriptor.external_id = access.externalId.trim();
    }
    if (access.sessionSeconds.trim() !== '') descriptor.session_seconds = Number(access.sessionSeconds.trim());
  }
  if (access.mode === 'static') {
    descriptor.access_key_id = access.accessKeyId.trim();
    descriptor.secret_access_key = access.secretAccessKey;
  }
  return JSON.stringify(descriptor);
}

/** The minimum a mode needs before the form may submit; the server validates the rest. */
export function awsAccessComplete(access: AwsAccess): boolean {
  switch (access.mode) {
    case 'ambient':
      return true;
    case 'assume-role':
    case 'web-identity':
      // A non-integer duration would serialize as null and be sent silently.
      return access.roleArn.trim() !== '' && /^\d*$/.test(access.sessionSeconds.trim());
    case 'static':
      return access.accessKeyId.trim() !== '' && access.secretAccessKey !== '';
  }
}

export function AwsAccessFields({
  value,
  onChange,
}: {
  readonly value: AwsAccess;
  readonly onChange: (next: AwsAccess) => void;
}) {
  const set = (patch: Partial<AwsAccess>) => onChange({ ...value, ...patch });
  const role = value.mode === 'assume-role' || value.mode === 'web-identity';
  return (
    <fieldset className="adapters__form adapters__form--two">
      <legend className="field__label">AWS access</legend>
      <label className="field">
        <span className="field__label">Authentication</span>
        <select
          value={value.mode}
          onChange={(event) => {
            const next = modes.find((mode) => mode.value === event.target.value)?.value ?? 'assume-role';
            // Leaving static drops the secret key from component state.
            set({ mode: next, secretAccessKey: next === 'static' ? value.secretAccessKey : '' });
          }}
        >
          {modes.map((mode) => (
            <option key={mode.value} value={mode.value}>
              {mode.label}
            </option>
          ))}
        </select>
      </label>
      {value.mode === 'static' ? null : (
        <p className="field__hint">
          Uses this server&apos;s own AWS identity. The node operator must allow it with
          HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY=allow; no secret is stored.
        </p>
      )}
      {role ? (
        <Input
          label="Role ARN"
          mono
          value={value.roleArn}
          placeholder="arn:aws:iam::123456789012:role/hikyo-sync"
          onChange={(event) => set({ roleArn: event.target.value })}
          autoComplete="off"
        />
      ) : null}
      {value.mode === 'assume-role' ? (
        <Input
          label="External id"
          value={value.externalId}
          onChange={(event) => set({ externalId: event.target.value })}
          autoComplete="off"
          hint="Optional. Required when the role's trust policy names one."
        />
      ) : null}
      {role ? (
        <Input
          label="Session duration (seconds)"
          inputMode="numeric"
          value={value.sessionSeconds}
          placeholder="900"
          onChange={(event) => set({ sessionSeconds: event.target.value })}
          hint="900 to 3600. Credentials refresh automatically."
        />
      ) : null}
      {value.mode === 'static' ? (
        <>
          <Input
            label="Access key id"
            mono
            value={value.accessKeyId}
            onChange={(event) => set({ accessKeyId: event.target.value })}
            autoComplete="off"
          />
          <Input
            label="Secret access key"
            type="password"
            value={value.secretAccessKey}
            onChange={(event) => set({ secretAccessKey: event.target.value })}
            autoComplete="new-password"
            hint="Write-only. It is sealed on save and never shown again."
          />
        </>
      ) : null}
      <Input
        label="Region"
        value={value.region}
        placeholder="eu-west-1"
        onChange={(event) => set({ region: event.target.value })}
        autoComplete="off"
        hint="Only for a VPC endpoint origin; AWS regional endpoints carry their region."
      />
    </fieldset>
  );
}
