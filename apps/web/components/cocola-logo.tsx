import type { SVGProps } from "react";

/**
 * cocola brand mark — the Phosphor "Sparkle" (灵光) glyph rendered duotone:
 * a 20%-opacity fill for body plus a rounded stroke for the outline. The
 * classic four-point sparkle reads instantly as "AI / agent".
 *
 * By default both layers use the blue -> violet brand gradient. Pass `mono` to
 * paint the mark with `currentColor` instead, so it can inherit text color in
 * monochrome contexts (e.g. inside the primary-colored sidebar badge, dark
 * mode, or print) — the fill layer keeps its 20% opacity in either mode.
 */
export function CocolaLogo({
  mono = false,
  ...props
}: SVGProps<SVGSVGElement> & { mono?: boolean }) {
  const paint = mono ? "currentColor" : "url(#cocola-brand)";
  const sparkle =
    "M128 24 L150 106 L232 128 L150 150 L128 232 L106 150 L24 128 L106 106 Z";
  return (
    <svg
      viewBox="0 0 256 256"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="cocola"
      {...props}
    >
      {!mono ? (
        <defs>
          <linearGradient id="cocola-brand" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stopColor="#32A7FD" />
            <stop offset="1" stopColor="#7B48FC" />
          </linearGradient>
        </defs>
      ) : null}
      <path d={sparkle} fill={paint} opacity={0.2} />
      <path
        d={sparkle}
        fill="none"
        stroke={paint}
        strokeWidth={16}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
