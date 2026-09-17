import { Button } from '../ui/Button.tsx';

export function ProviderDiscoveryAlert({ onRetry }: { onRetry: () => void }) {
  return (
    <p className="alert" role="alert">
      <span className="alert__glyph" aria-hidden="true">
        !
      </span>
      <span>Identity provider options could not be loaded.</span>
      <Button type="button" onClick={onRetry}>
        Retry identity providers
      </Button>
    </p>
  );
}
