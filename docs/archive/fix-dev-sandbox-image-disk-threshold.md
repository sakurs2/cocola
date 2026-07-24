# fix: Raise the development sandbox image disk threshold

- Change time: 2026-07-24 15:45 (+08:00)

## Reason

The local development startup guard rejected a k3d sandbox image filesystem at 84% usage because its maximum was 80%. This prevented startup even though enough capacity remained for the already pre-pulled runtime image.

## Changes

- `scripts/run-stack-dev.sh`: raises the post-pull sandbox image filesystem usage limit from 80% to 90%.
- The existing fail-fast behavior remains unchanged: usage through 90% is accepted, while usage above 90% still stops startup and prints cleanup guidance.

## Notes

- This changes only the local `make dev` pre-pull guard.
- No Docker cache, image, or user data is modified.
