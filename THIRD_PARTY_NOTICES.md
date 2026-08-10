# Third-party notices

## Forgejo container dependency

Cocola deployments pull the Forgejo container image directly from its upstream
Codeberg registry for the internal SCM service:

- Upstream project: <https://codeberg.org/forgejo/forgejo>
- Version: `16.0.1`
- Upstream multi-architecture manifest digest:
  `sha256:3eb3107bc9de4e9d6d9e539044e6c802dc0b7be351919a145540d4cb5422bf07`
- License: GPL-3.0-or-later

Cocola does not rebuild, mirror, modify, or claim authorship of Forgejo. The
image reference includes the upstream manifest digest so the deployed content
cannot drift if the version tag changes.

Forgejo is a trademark of its respective owners and is not part of the Cocola
source code licensed under Apache-2.0.
