# OpenPencil `openpencil://` URL Scheme Implementation Plan (upstream contribution)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a web page (the published Hikyo Storybook) open a design file and select one node in the OpenPencil desktop app through `openpencil://open?file=<repo-relative>&node=<name path>`.

**Architecture:** `tauri-plugin-deep-link` registers the `openpencil` scheme. The Rust side parses the URL into a `PendingOpenFile { path, node }` and reuses the existing `queue_open_paths` → `open-associated-files` → `take_pending_open` pipeline. The Vue side resolves a relative `file` against open tabs, then remembered roots, then a one-time picker, and after opening selects the node by exact name and zooms to fit. The scheme carries no authority: it can only open a file the user already opened or picks, and select.

**Tech Stack:** Tauri 2, `tauri-plugin-deep-link` 2.4.10, Rust, Vue 3, Bun. Repo: `~/code/homelab/open-pencil` (upstream `open-pencil/open-pencil`, main at `c29654c`, version 0.14.0).

**Spec:** `docs/superpowers/specs/2026-09-16-openpencil-storybook-design.md` §3 (in the Hikyo repo). This plan lives in Hikyo's docs because the Hikyo Storybook depends on it; the code goes to an upstream PR.

## Global Constraints

- Work on a branch of the open-pencil checkout: `git checkout -b feat/url-scheme` from a freshly pulled `main`. Follow upstream `CONTRIBUTING.md` and `AGENTS.md` (read them first; oxlint/oxfmt and `bun run lint` must pass).
- Upstream commit style: read the last 20 commits (`git log --oneline -20`) and match.
- Absolute paths in the URL are refused. Only `file` values ending in `.pen` or `.fig` are accepted. `..` segments are refused.
- No new permissions beyond `deep-link:default`.
- The scheme handler must not create, write, export, or run anything.
- Every step that touches Rust ends with `cargo check` in `desktop/`; every step touching Vue ends with `bun run lint`.

---

## File structure

| Path (in open-pencil) | Responsibility |
| --- | --- |
| `desktop/Cargo.toml` | Add `tauri-plugin-deep-link = "2.4.10"`. |
| `desktop/tauri.conf.json` | `plugins.deep-link.desktop.schemes = ["openpencil"]`. |
| `desktop/capabilities/default.json` | Add `deep-link:default`. |
| `desktop/src/deep_link.rs` | Pure parser: URL → `Result<DeepLinkOpen, DeepLinkError>`; unit tests. |
| `desktop/src/lib.rs` | Register plugin, feed parsed links into `PendingOpen`; `PendingOpenFile` gains `node: Option<String>`. |
| `src/app/document/io/deep-link.ts` | Resolve relative file against open tabs / remembered roots / picker; select node by name after open. |
| `src/views/EditorView.vue` | Use the resolver for pending files carrying `node` or a relative path. |
| `packages/docs/en/…/mcp-server.md` (or the nearest "programmable" page) | Document the scheme. |

---

### Task 1: Plugin registration

**Files:**
- Modify: `desktop/Cargo.toml` (dependencies block after `tauri-plugin-clipboard-manager`)
- Modify: `desktop/tauri.conf.json` (`plugins`)
- Modify: `desktop/capabilities/default.json` (`permissions`)

- [ ] **Step 1: Cargo**

Add under `[dependencies]`:
```toml
tauri-plugin-deep-link = "2.4.10"
```

- [ ] **Step 2: tauri.conf.json**

Inside the existing `"plugins": { … }` object add:
```json
    "deep-link": {
      "desktop": { "schemes": ["openpencil"] }
    }
```

- [ ] **Step 3: Capability**

Add `"deep-link:default"` to the `permissions` array in `desktop/capabilities/default.json`.

- [ ] **Step 4: Check**

```bash
cd desktop && cargo check
```
Expected: compiles (plugin not yet initialised, that is Task 3).

- [ ] **Step 5: Commit**

```bash
git add desktop/Cargo.toml desktop/Cargo.lock desktop/tauri.conf.json desktop/capabilities/default.json
git commit -s -m "feat(desktop): register openpencil:// deep link scheme"
```

---

### Task 2: URL parser with tests (Rust, TDD)

**Files:**
- Create: `desktop/src/deep_link.rs`
- Modify: `desktop/src/lib.rs` (add `mod deep_link;`)

**Interfaces:**
- Produces:
  ```rust
  pub struct DeepLinkOpen { pub file: String, pub node: Option<String> }
  pub enum DeepLinkError { UnknownAction(String), MissingFile, AbsolutePath, ParentSegment, BadExtension, BadUrl }
  pub fn parse_open_url(url: &url::Url) -> Result<DeepLinkOpen, DeepLinkError>
  ```

- [ ] **Step 1: Failing tests**

```rust
// desktop/src/deep_link.rs
use url::Url;

#[derive(Debug, PartialEq, Eq)]
pub struct DeepLinkOpen {
    pub file: String,
    pub node: Option<String>,
}

#[derive(Debug, PartialEq, Eq)]
pub enum DeepLinkError {
    UnknownAction(String),
    MissingFile,
    AbsolutePath,
    ParentSegment,
    BadExtension,
}

pub fn parse_open_url(url: &Url) -> Result<DeepLinkOpen, DeepLinkError> {
    todo!()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(s: &str) -> Result<DeepLinkOpen, DeepLinkError> {
        parse_open_url(&Url::parse(s).unwrap())
    }

    #[test]
    fn open_with_file_and_node() {
        assert_eq!(
            parse("openpencil://open?file=web%2Fdesign%2Fhikyo.pen&node=Button%2FPrimary"),
            Ok(DeepLinkOpen { file: "web/design/hikyo.pen".into(), node: Some("Button/Primary".into()) })
        );
    }

    #[test]
    fn node_is_optional() {
        assert_eq!(parse("openpencil://open?file=a.pen").unwrap().node, None);
    }

    #[test]
    fn unknown_action() {
        assert_eq!(parse("openpencil://export?file=a.pen"), Err(DeepLinkError::UnknownAction("export".into())));
    }

    #[test]
    fn missing_file() {
        assert_eq!(parse("openpencil://open"), Err(DeepLinkError::MissingFile));
    }

    #[test]
    fn absolute_refused() {
        assert_eq!(parse("openpencil://open?file=%2FUsers%2Fx%2Fa.pen"), Err(DeepLinkError::AbsolutePath));
        assert_eq!(parse("openpencil://open?file=C%3A%5Cx%5Ca.pen"), Err(DeepLinkError::AbsolutePath));
    }

    #[test]
    fn parent_segment_refused() {
        assert_eq!(parse("openpencil://open?file=..%2Fa.pen"), Err(DeepLinkError::ParentSegment));
    }

    #[test]
    fn extension_checked() {
        assert_eq!(parse("openpencil://open?file=a.txt"), Err(DeepLinkError::BadExtension));
        assert!(parse("openpencil://open?file=a.fig").is_ok());
    }
}
```
Add `mod deep_link;` near the other `mod` lines in `desktop/src/lib.rs`. `url` is already a transitive dependency through tauri; if `cargo check` says otherwise, add `url = "2"` to `[dependencies]`.

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop && cargo test deep_link
```
Expected: panics on `todo!()`.

- [ ] **Step 3: Implement**

```rust
pub fn parse_open_url(url: &Url) -> Result<DeepLinkOpen, DeepLinkError> {
    // openpencil://open?…  → host is the action.
    let action = url.host_str().unwrap_or("");
    if action != "open" {
        return Err(DeepLinkError::UnknownAction(action.to_string()));
    }
    let mut file = None;
    let mut node = None;
    for (k, v) in url.query_pairs() {
        match k.as_ref() {
            "file" => file = Some(v.into_owned()),
            "node" => node = Some(v.into_owned()),
            _ => {}
        }
    }
    let file = file.filter(|f| !f.is_empty()).ok_or(DeepLinkError::MissingFile)?;
    let is_windows_abs = file.len() > 1 && file.as_bytes()[1] == b':';
    if file.starts_with('/') || file.starts_with('\\') || is_windows_abs {
        return Err(DeepLinkError::AbsolutePath);
    }
    if file.split(['/', '\\']).any(|seg| seg == "..") {
        return Err(DeepLinkError::ParentSegment);
    }
    let lower = file.to_ascii_lowercase();
    if !(lower.ends_with(".pen") || lower.ends_with(".fig")) {
        return Err(DeepLinkError::BadExtension);
    }
    Ok(DeepLinkOpen { file, node: node.filter(|n| !n.is_empty()) })
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cargo test deep_link
```
Expected: 7 passed.

- [ ] **Step 5: Commit**

```bash
git add desktop/src/deep_link.rs desktop/src/lib.rs
git commit -s -m "feat(desktop): parse openpencil://open?file&node links"
```

---

### Task 3: Feed links into the pending-open pipeline

**Files:**
- Modify: `desktop/src/lib.rs:29-36` (`PendingOpenFile`), `:84-107` (`queue_open_paths`), `:118-135` (builder), `:158-166` (`RunEvent::Opened`)

**Interfaces:**
- `PendingOpenFile { path: String, node: Option<String> }` (serde, `node` skipped when `None`).
- New `fn queue_deep_links<R: tauri::Runtime>(app: &tauri::AppHandle<R>, urls: Vec<url::Url>)`.

- [ ] **Step 1: Extend the struct**

```rust
#[derive(Clone, serde::Serialize)]
struct PendingOpenFile {
    path: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    node: Option<String>,
}
```
In `queue_open_paths` set `node: None`.

- [ ] **Step 2: Add the deep-link queue**

```rust
fn queue_deep_links<R: tauri::Runtime>(app: &tauri::AppHandle<R>, urls: Vec<url::Url>) {
    let files: Vec<PendingOpenFile> = urls
        .iter()
        .filter(|u| u.scheme() == "openpencil")
        .filter_map(|u| match deep_link::parse_open_url(u) {
            Ok(open) => Some(PendingOpenFile { path: open.file, node: open.node }),
            Err(e) => {
                eprintln!("[deep-link] refused {u}: {e:?}");
                None
            }
        })
        .collect();
    if files.is_empty() {
        return;
    }
    if let Ok(mut pending) = app.state::<PendingOpen>().0.lock() {
        pending.extend(files);
    }
    let _ = app.emit("open-associated-files", ());
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.set_focus();
    }
}
```
Relative paths are deliberately not passed through `fs_scope().allow_file`: the frontend resolves them and the resolved absolute path is allowed there (Task 4 step 3) through the existing `openFileFromPath` flow.

- [ ] **Step 3: Initialise the plugin and hook events**

In `run()`, after the single-instance block:
```rust
    builder = builder.plugin(tauri_plugin_deep_link::init());
```
In `.setup(|app| { … })`, before `Ok(install_app_menu(app)?)`:
```rust
            use tauri_plugin_deep_link::DeepLinkExt;
            #[cfg(any(windows, target_os = "linux"))]
            {
                let _ = app.deep_link().register_all();
            }
            let handle = app.handle().clone();
            app.deep_link().on_open_url(move |event| {
                queue_deep_links(&handle, event.urls());
            });
```
In the macOS `RunEvent::Opened { urls }` arm, split scheme URLs from file URLs:
```rust
            tauri::RunEvent::Opened { urls } => {
                let (links, files): (Vec<_>, Vec<_>) = urls.into_iter().partition(|u| u.scheme() == "openpencil");
                queue_deep_links(_app, links);
                let paths = files
                    .into_iter()
                    .filter_map(|url| url.to_file_path().ok())
                    .filter_map(file_association_path)
                    .collect();
                queue_open_paths(_app, paths);
            }
```
`on_open_url` also fires on macOS through the plugin; if both paths deliver the same URL during manual testing (step 5), keep only the plugin path and drop the partition.

- [ ] **Step 4: Check**

```bash
cargo check && cargo test
```

- [ ] **Step 5: Manual smoke (macOS)**

```bash
bun run tauri dev
open "openpencil://open?file=tests%2Ffixtures%2Fpencil_button.pen&node=Button%2FLarge%2FDefault"
```
Expected: the app focuses and, until Task 4, logs a pending file with a relative path (open the devtools console; `take_pending_open` returns it). Note whether the URL arrived once or twice.

- [ ] **Step 6: Commit**

```bash
git add desktop/src/lib.rs
git commit -s -m "feat(desktop): queue openpencil:// links as pending opens"
```

---

### Task 4: Frontend resolution and node selection

**Files:**
- Create: `src/app/document/io/deep-link.ts`
- Modify: `src/views/EditorView.vue:70-90`

**Interfaces:**
- Consumes: `openFileFromPath(path)` from `@/app/shell/menu/files`; `chooseTauriOpenPath()` from the same module; the open-documents store used by `openFileInNewTab` (find it with `grep -rn "openFileInNewTab" src/app/document` and read how tabs expose their `path`); the editor's selection and viewport commands (find with `grep -rn "zoomToFit\|zoom-to-fit" src/app src/components/editor` and `grep -rn "currentPage.selection" src/`).
- Produces:
  ```ts
  export function resolveDeepLinkFile(file: string, ctx: { openPaths: string[]; roots: string[]; exists: (p: string) => boolean }): string | null
  export function rememberRoot(file: string, absolute: string, roots: string[]): string[]
  export async function openDeepLink(pending: { path: string; node?: string }): Promise<void>
  ```

- [ ] **Step 1: Failing tests for the pure resolver**

```ts
// src/app/document/io/deep-link.test.ts
import { describe, expect, it } from 'bun:test'; // or vitest, whichever upstream uses (check an existing *.test.ts)

import { rememberRoot, resolveDeepLinkFile } from './deep-link';

describe('resolveDeepLinkFile', () => {
  it('prefers an open tab whose path ends with the relative file', () => {
    expect(resolveDeepLinkFile('web/design/hikyo.pen', { openPaths: ['/r/hikyo/web/design/hikyo.pen'], roots: [], exists: () => true })).toBe('/r/hikyo/web/design/hikyo.pen');
  });
  it('falls back to a remembered root', () => {
    expect(resolveDeepLinkFile('web/design/hikyo.pen', { openPaths: [], roots: ['/r/hikyo'], exists: () => true })).toBe('/r/hikyo/web/design/hikyo.pen');
  });
  it('returns null when nothing matches', () => {
    expect(resolveDeepLinkFile('web/design/hikyo.pen', { openPaths: ['/other/x.pen'], roots: [], exists: () => true })).toBeNull();
  });
  it('does not match a partial segment', () => {
    expect(resolveDeepLinkFile('design/hikyo.pen', { openPaths: ['/r/redesign/hikyo.pen'], roots: [], exists: () => true })).toBeNull();
  });
});

describe('rememberRoot', () => {
  it('derives the root by stripping the relative file and dedupes', () => {
    expect(rememberRoot('web/design/hikyo.pen', '/r/hikyo/web/design/hikyo.pen', ['/r/hikyo'])).toEqual(['/r/hikyo']);
    expect(rememberRoot('a.pen', '/x/a.pen', [])).toEqual(['/x']);
  });
});
```

- [ ] **Step 2: Implement the module**

```ts
// src/app/document/io/deep-link.ts
import { chooseTauriOpenPath, openFileFromPath } from '@/app/shell/menu/files';

const ROOTS_KEY = 'openpencil.deepLinkRoots';

function endsWithSegments(absolute: string, relative: string) {
  const a = absolute.replaceAll('\\', '/');
  const r = relative.replaceAll('\\', '/');
  return a === r || a.endsWith(`/${r}`);
}

export function resolveDeepLinkFile(
  file: string,
  ctx: { openPaths: string[]; roots: string[]; exists: (p: string) => boolean },
): string | null {
  const open = ctx.openPaths.find((p) => endsWithSegments(p, file));
  if (open) return open;
  for (const root of ctx.roots) {
    const candidate = `${root.replace(/[/\\]$/, '')}/${file}`;
    if (ctx.exists(candidate)) return candidate;
  }
  return null;
}

export function rememberRoot(file: string, absolute: string, roots: string[]): string[] {
  const a = absolute.replaceAll('\\', '/');
  const root = a.slice(0, a.length - file.replaceAll('\\', '/').length - 1);
  return roots.includes(root) ? roots : [...roots, root];
}

function loadRoots(): string[] {
  try {
    const raw = localStorage.getItem(ROOTS_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) && parsed.every((x) => typeof x === 'string') ? parsed : [];
  } catch {
    return [];
  }
}

export async function openDeepLink(pending: { path: string; node?: string }, deps: {
  openPaths: () => string[];
  selectByName: (name: string) => boolean; // selects + zooms; false when not found
  notify: (message: string) => void;
  exists: (p: string) => boolean;
}) {
  let target = resolveDeepLinkFile(pending.path, { openPaths: deps.openPaths(), roots: loadRoots(), exists: deps.exists });
  if (!target) {
    deps.notify(`Locate ${pending.path} for this link`);
    const picked = await chooseTauriOpenPath();
    if (!picked || !endsWithSegments(picked, pending.path)) {
      deps.notify(`Link cancelled: expected a file ending in ${pending.path}`);
      return;
    }
    localStorage.setItem(ROOTS_KEY, JSON.stringify(rememberRoot(pending.path, picked, loadRoots())));
    target = picked;
  }
  await openFileFromPath(target);
  if (pending.node && !deps.selectByName(pending.node)) {
    deps.notify(`Node "${pending.node}" not found in ${pending.path}`);
  }
}
```
`exists` is injected: the unit tests pass `() => true`; `openDeepLink` passes a wrapper over `exists` from `@tauri-apps/plugin-fs` (async, so resolve candidates before calling the pure function, or make the function async; keep the tests in step with whichever you pick).

- [ ] **Step 3: Wire EditorView**

In `src/views/EditorView.vue`, extend the type and the loop:
```ts
type PendingOpenFile = { path: string; node?: string }

async function openPendingAssociatedFiles() {
  const { invoke } = await import('@tauri-apps/api/core')
  const files = await invoke<PendingOpenFile[]>('take_pending_open')
  for (const file of files) {
    const isRelative = !file.path.startsWith('/') && !/^[A-Za-z]:[\\/]/.test(file.path)
    if (isRelative || file.node) {
      await openDeepLink(file, { openPaths: () => documents.value.map((d) => d.path).filter(Boolean), selectByName, notify })
    } else {
      await openFileFromPath(file.path)
    }
  }
}
```
`documents`, `selectByName`, `notify`: bind to the real stores found in the Interfaces step. `selectByName` = find on the current page by exact `name`, set the selection, run the existing zoom-to-fit command. Absolute paths reaching this branch from a deep link are impossible (parser refuses them), so `openFileFromPath` stays the file-association path only.

- [ ] **Step 4: Tests, lint, smoke**

```bash
bun test src/app/document/io/deep-link.test.ts
bun run lint
bun run tauri dev
open "openpencil://open?file=tests%2Ffixtures%2Fpencil_button.pen&node=Button%2FLarge%2FDefault"
```
Expected: first click prompts the picker once; picking `tests/fixtures/pencil_button.pen` opens it and selects the button; second click opens directly. A bad node shows the "not found" notice.

- [ ] **Step 5: Commit**

```bash
git add src/app/document/io/deep-link.ts src/app/document/io/deep-link.test.ts src/views/EditorView.vue
git commit -s -m "feat(app): resolve openpencil:// links and select the target node"
```

---

### Task 5: Docs, changelog, PR

- [ ] **Step 1:** Add a "URL scheme" subsection to the programmable docs page next to the MCP server section (English source; other locales follow upstream's translation process, check `packages/docs` README). Content: the URL format, the relative-path rule, the one-time picker, and that the scheme only opens and selects.
- [ ] **Step 2:** `CHANGELOG.md` entry under Unreleased.
- [ ] **Step 3:** `bun run lint && bun run format:check && cargo test` in `desktop/`.
- [ ] **Step 4:** Push the branch to a fork (`gh repo fork --remote`), open the PR against `open-pencil/open-pencil` with the smoke steps and a short screen recording. Link the PR in the Hikyo handoff doc and in the Hikyo follow-up issue for middleware removal.
