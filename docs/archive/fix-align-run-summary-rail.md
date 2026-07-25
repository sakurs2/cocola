# fix: Align Run Summary with the response rail

- Change time: 2026-07-25 13:25 (+08:00)

## Reason

The Run Summary used an inline clock icon outside the shared response timeline grid. Its center appeared to the left of the Answer and tool rail, and the clock emphasized duration rather than the overall Run outcome and activity.

## Changes

- `apps/web/components/assistant-ui/rich-message-parts.tsx`: replaces the clock with an Activity icon.
- The Summary header and expanded details now use the same `1.75rem` icon column and spacing as the shared response rail.
- The icon center aligns with the vertical rail without negative margins or position-specific offsets.
