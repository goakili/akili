# Akili for VS Code

Chat with [Akili](https://github.com/goakili/akili) agents, run coding tasks, approve their actions
and review their changes without leaving the editor.

Akili is a security-first control plane for autonomous AI agents. This extension is a client of your
control plane: the agents, their policies, approvals and the audit log stay there. The extension
holds no model or forge keys and decides nothing on its own.

## What it does

- **Chat** with an agent about the linked project, in the Akili sidebar. Tool calls, change plans
  and approvals appear as they do in the web UI.
- **Tasks**: start a task on the project, follow its status, open its diff, or **check out** its
  `akili/<task>` branch to review it locally.
- **Approvals**: when an agent in one of your sessions waits for a decision, a notification shows
  the exact call. **Approve** shows the full arguments before it decides.
- **Status bar**: the server, the linked project and pending approvals.

Agents run on your Akili fleet and work on their own clone of the repository; their work reaches
you as a branch and a pull request.

## Getting started

1. Run **Akili: Sign in** from the Command Palette and enter your control plane URL.
2. Approve the sign-in in your browser. Akili sends a one-time code back to the editor, which
   exchanges it (with PKCE) for an API key that acts as you for 90 days. The key is kept in the
   editor's secret storage; revoke it any time under **Settings → API keys** in Akili.
3. Open a repository. If it matches an Akili project (by its git remote), the extension offers to
   link it. Otherwise run **Akili: Link this folder to a project**.

Editors without a working URI handler can use **Akili: Sign in with an API key** instead.

### Sharing the link with your team

Commit an `.akili.json` with the project's slug:

```json
{ "project": "simple-api" }
```

Only the slug is read. The control plane URL always comes from your own user settings
(`akili.url`), so a repository can't point the extension at another server.

## Security

- The sidebar runs in a webview with a strict content security policy and **no network access**.
  Its API calls go through the extension, which forwards only the calls the sidebar needs (chat,
  tasks, approvals): never users, policies, keys or terminals.
- In an untrusted workspace the extension doesn't link projects or run git.
- `git` runs only to read the remote and to fetch and check out `akili/<task>` branches, and it
  refuses to switch branches over uncommitted changes.

## Development

From the repository root:

```sh
make vscode        # builds the sidebar (web/src/vscode) and the extension
make vscode-test   # unit tests
make vscode-package
```

Press F5 in `vscode/` to run it in an Extension Development Host.
