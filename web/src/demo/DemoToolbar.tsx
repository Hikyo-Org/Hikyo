import { Button } from '../ui/Button.tsx';
import { DEMO_ACCOUNTS, DEMO_MATRIX_PATH, demoRole } from './config.ts';
import './demo.css';

export function DemoToolbar() {
  const role = demoRole();
  const account = DEMO_ACCOUNTS.find((candidate) => candidate.id === role);
  return (
    <aside className="demo-toolbar" aria-label="Demo controls">
      <div className="demo-toolbar__controls">
        <strong className="demo-toolbar__badge">Demo</strong>
        <label htmlFor="demo-account">Try as</label>
        <select
          id="demo-account"
          value={role}
          aria-describedby="demo-role"
          onChange={(event) => {
            const next = new URL(window.location.href);
            next.searchParams.set('account', event.target.value);
            next.hash = DEMO_MATRIX_PATH;
            window.location.assign(next.href);
          }}
        >
          {DEMO_ACCOUNTS.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>{candidate.label}</option>
          ))}
        </select>
        <Button type="button" onClick={() => window.location.reload()}>Reset demo</Button>
        <span className="demo-toolbar__note">Sample data only. Changes reset on reload.</span>
      </div>
      <p id="demo-role">{account?.description}</p>
    </aside>
  );
}
