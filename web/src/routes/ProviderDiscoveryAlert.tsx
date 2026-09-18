import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';

export function ProviderDiscoveryAlert({ onRetry }: { onRetry: () => void }) {
  return (
    <Alert
      action={
        <Button type="button" onClick={onRetry}>
          Retry identity providers
        </Button>
      }
    >
      Identity provider options could not be loaded.
    </Alert>
  );
}
