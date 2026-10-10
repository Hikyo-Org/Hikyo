import { zPkiIssuerList, zPkiProfileList } from '@hikyo/zod';
import type { z } from 'zod';

const now = '2026-09-01T00:00:00Z';
export const issuers = { issuers: [
  { id: 'pkii_00000000-0000-7000-8000-000000000001', name: 'issuing', version: 1, kind: 'intermediate', origin: 'generated', state: 'active', key_algorithm: 'ecdsa-p256', key_fingerprint: 'sha256:public-fixture', certificate_pem: '-----BEGIN CERTIFICATE-----\nPUBLIC FIXTURE\n-----END CERTIFICATE-----', subject_cn: 'Service issuing authority', restore_hold: false, issued_count: 3, crl_number: 0, created_at: now, updated_at: now },
  { id: 'pkii_00000000-0000-7000-8000-000000000002', name: 'offline', version: 1, kind: 'intermediate', origin: 'generated', state: 'pending', key_algorithm: 'ecdsa-p256', key_fingerprint: 'sha256:pending-fixture', csr_pem: '-----BEGIN CERTIFICATE REQUEST-----\nPUBLIC FIXTURE\n-----END CERTIFICATE REQUEST-----', subject_cn: 'Offline signed intermediate authority for production services', restore_hold: true, issued_count: 0, crl_number: 0, created_at: now, updated_at: now },
] } satisfies z.input<typeof zPkiIssuerList>;
export const profiles = { profiles: [{
  id: 'pkip_00000000-0000-7000-8000-000000000001', name: 'web-services', row_version: 1, created_at: now, updated_at: now,
  bindings: [{ id: 'pkib_00000000-0000-7000-8000-000000000001', org_id: 'org_acme', project_id: 'prj_app', environment_id: 'env_prod', created_at: now }],
  policy: { allowed_issuers: ['issuing'], dns_patterns: ['*.svc.example.com'], ip_ranges: [], uri_patterns: [], allow_wildcard_names: false, key_algorithms: ['ecdsa-p256'], key_usages: ['digital-signature'], ext_key_usages: ['server-auth'], max_ttl_seconds: 259200, default_ttl_seconds: 86400, renew_window_seconds: 28800, allow_csr: true, allow_generated_key: false, machine_issuance: false, organization: '' },
}] } satisfies z.input<typeof zPkiProfileList>;
