# fix: Restore the development sandbox image disk guard

- Change time: 2026-07-25 13:25 (+08:00)

## Reason

Allowing the local k3d sandbox image filesystem to reach 90% usage left too little headroom for containerd. Kubelet could garbage-collect the pre-pulled runtime image, causing later Sandbox acquisition to time out while pulling the image again.

## Changes

- `scripts/run-stack-dev.sh`: restores the post-pull sandbox image filesystem usage limit from 90% to 80%.
- The existing fail-fast cleanup guidance remains unchanged.

## Notes

- This guard only affects local `make dev` startup.
- The change does not delete Docker data or build a Sandbox image.
