import { useSyncExternalStore } from 'react';

type Notification = {
  id: number;
  message: string;
  tone: 'error' | 'info' | 'success';
  action?: { href: string; label: string };
  onDismiss?: () => void;
};

let current: Notification | null = null;
let nextId = 1;
const listeners = new Set<() => void>();

function publish(
  tone: Notification['tone'],
  message: string,
  action?: Notification['action'],
  onDismiss?: () => void,
): void {
  current = { id: nextId, message, tone, action, onDismiss };
  nextId += 1;
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function snapshot(): Notification | null {
  return current;
}

export function notifyFailure(message: string): void {
  publish('error', message);
}

export function notifySuccess(message: string): void {
  publish('success', message);
}

export function notifyUpdate(message: string, href: string, onDismiss: () => void): void {
  publish('info', message, { href, label: 'View release' }, onDismiss);
}

export function clearNotification(): void {
  if (current === null) {
    return;
  }
  current = null;
  for (const listener of listeners) {
    listener();
  }
}

function dismiss(id: number): void {
  if (current?.id !== id) {
    return;
  }
  current.onDismiss?.();
  clearNotification();
}

export function ToastViewport() {
  const notification = useSyncExternalStore(subscribe, snapshot, snapshot);
  if (notification === null) {
    return null;
  }
  return (
    <div
      className={`toast toast--${notification.tone}`}
      role={notification.tone === 'error' ? 'alert' : 'status'}
    >
      <span className="alert__glyph" aria-hidden="true">
        {notification.tone === 'error' ? '!' : notification.tone === 'success' ? '✓' : '↑'}
      </span>
      <span>{notification.message}</span>
      {notification.action === undefined ? null : (
        <a
          className="toast__action"
          href={notification.action.href}
          target="_blank"
          rel="noreferrer"
        >
          {notification.action.label}
        </a>
      )}
      <button
        type="button"
        className="toast__dismiss"
        aria-label="Dismiss notification"
        onClick={() => dismiss(notification.id)}
      >
        ×
      </button>
    </div>
  );
}
