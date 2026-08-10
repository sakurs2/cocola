# Third-party notices

## Forgejo container redistribution

Cocola redistributes an unmodified copy of the upstream Forgejo container image
for reliable internal SCM installation:

- Upstream project: <https://codeberg.org/forgejo/forgejo>
- Version: `16.0.1`
- Upstream tag commit: `b3d7e4ac3cbccc220703097a51fa4c16bf302579`
- Upstream multi-architecture manifest digest:
  `sha256:3eb3107bc9de4e9d6d9e539044e6c802dc0b7be351919a145540d4cb5422bf07`
- License: GPL-3.0-or-later, as distributed in the corresponding upstream
  source archive

The mirrored image is copied byte-for-byte between OCI registries. Cocola does
not rebuild, modify, or claim authorship of Forgejo. Every Cocola release that
depends on this image includes `forgejo-16.0.1-source.tar.gz` and its provenance
record as stable release assets, providing the complete corresponding source
and the exact GPL license distributed by Forgejo.

Forgejo is a trademark of its respective owners and is not part of the Cocola
source code licensed under Apache-2.0.
