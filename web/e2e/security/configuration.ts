/** This is a bounded markup/CORS campaign using native auth over dev transport.
 * The receipt states the broader seed's two deliberate disclosure settings.
 */
export const SECURITY_FIXTURE_CONFIGURATION = Object.freeze({
  transport: 'loopback-http-dev',
  admission_limits: 'product-default',
  service_budgets: 'product-default',
  fake_adapter_provider: false,
  child_environment: 'allowlist',
  development_disclosure_window_seconds: 300,
  protected_disclosure_window_seconds: 0,
  external_identity_providers: false,
});
