# Fix: side-nav sections (Kubernetes, Cloudflare, Ansible, Terraform, Repositories, Shortcuts, Secrets) not showing

**Date**: 2026-11-23

## Root cause

`internal/web/static/index.html`: the Docker section header (`#sectionDocker > .section-header`) was missing its closing `</div>` (since commit 671aaa4). The `</div>` that followed the header closed the header instead of `#sectionDocker`, so every later section (`sectionKube` through `sectionTerraform`) was nested inside `#sectionDocker`. `switchSideNav()` shows a section by setting its own `display: block`, but its parent stayed `display: none`, so the content never rendered. The active tab highlight still changed, which made it look like the menu worked but showed an error or an empty area.

## Change

- Added the missing `</div>` closing `.section-header` in the Docker section.
- Removed the stray `</div>` that had been compensating for it before `<!-- Activity Console Drawer -->`.

Result: all section containers are direct children of `#appView`, the same as before the regression for the rest of the document.

## Verification

- Structure check: parsed `index.html` before and after. Every `section*` element now has parent `appView`; the activity drawer and the rest of the tree are unchanged.
- e2e (`npx e2e run`):
  - `shortcuts`, `vault`, `kube-workspaces`, `repos` (6 tests): were failing, now pass.
  - `docker-security`, `host-view-mode`: pass (regression check).
  - `ansible.e2e.ts`: still fails on `ansible-playbook not found` vs. expected `Ansible Gate is off`. This is environmental: `ansible-playbook` is not installed on this machine. Not caused by this change.

## Notes

- The change is in `index.html` only; the static files are embedded in the Go binary, so the running Pantau instance must be rebuilt/restarted to pick it up.
- Not committed.
