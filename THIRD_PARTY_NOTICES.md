# Third-party container image notices

Cocola redistributes unmodified, digest-pinned, multi-platform copies of the
following upstream container images under `ghcr.io/sakurs2`. Cocola does not
rebuild these images or claim authorship of the upstream projects.

| Cocola image                | Upstream                                              | License            |
| --------------------------- | ----------------------------------------------------- | ------------------ |
| `cocola-redis`              | [Redis](https://github.com/redis/redis)               | RSALv2 or SSPLv1   |
| `cocola-postgres`           | [PostgreSQL](https://github.com/postgres/postgres)    | PostgreSQL License |
| `cocola-forgejo`            | [Forgejo](https://codeberg.org/forgejo/forgejo)       | GPL-3.0-or-later   |
| `cocola-minio`              | [MinIO](https://github.com/minio/minio)               | AGPL-3.0           |
| `cocola-minio-mc`           | [MinIO Client](https://github.com/minio/mc)           | AGPL-3.0           |
| `cocola-opensandbox-server` | [OpenSandbox](https://github.com/alibaba/OpenSandbox) | Apache-2.0         |
| `cocola-opensandbox-egress` | [OpenSandbox](https://github.com/alibaba/OpenSandbox) | Apache-2.0         |

The exact upstream images, source commits, build-recipe commits, licenses,
manifest digests, source archive names, and archive checksums are recorded in
[`deploy/third-party-images.lock.json`](./deploy/third-party-images.lock.json).
Each lock revision has a corresponding `third-party-images-<revision>` GitHub
Release containing the verified source and license bundle.

OpenViking remains distributed by its upstream project under
`ghcr.io/volcengine/openviking`; Cocola changes only the registry hostname when
the operator explicitly selects a compatible GHCR proxy endpoint.

All project names and trademarks belong to their respective owners and are not
part of the Cocola source code licensed under Apache-2.0.
