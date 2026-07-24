# fix: Keep the Slash menu from shifting the Composer

- Change time: 2026-07-25 00:14 (+08:00)

## Reason

Typing `/` on the main Web Composer mounted the assistant-ui TriggerPopover
only after the trigger became active. Trigger behavior registration completes
in an effect, so the custom tab strip briefly rendered as a normal-flow sibling
before the popover wrapper became active. That intermediate frame displaced the
Composer and appeared as a visible shake.

## Changes

- `apps/web/components/assistant-ui/thread.tsx`: keeps the Slash TriggerPopover
  registered before it is opened.
- Moves all visible menu chrome into `TriggerPopoverItems`, whose lifecycle
  renders nothing while the trigger is closed and renders directly inside the
  positioned popover when `/` is active.
- Removes the redundant composer-text mount guard without changing command,
  skill, keyboard-navigation, or selected-skill behavior.
