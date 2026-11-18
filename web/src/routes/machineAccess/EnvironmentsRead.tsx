import { Alert } from '../../ui/Alert.tsx';

/** How far the project's environment list has been read: a failed read is not an empty list. */
export type EnvironmentsRead = 'loading' | 'unavailable' | 'ready';

/**
 * NoEnvironments is what a per-environment tab says while it has no environment
 * to show. Only a completed read may claim the project has none; a read in
 * flight or refused says so instead.
 */
export function NoEnvironments({ read, what }: { read: EnvironmentsRead; what: string }) {
  if (read === 'loading') return <p role="status">Loading environments…</p>;
  if (read === 'unavailable') {
    return (
      <Alert>
        The project&apos;s environments could not be read, so its {what} cannot be listed. Reload
        to try again.
      </Alert>
    );
  }
  return <p role="status">This project has no environments, so it has no {what}.</p>;
}
