"use client";

import { useId } from "react";

export function CocolaCoreLogo({ className }: { className?: string }) {
  const gradientId = useId().replaceAll(":", "");
  const sparkle = "M128 24 L150 106 L232 128 L150 150 L128 232 L106 150 L24 128 L106 106 Z";

  return (
    <svg
      aria-hidden="true"
      className={className}
      viewBox="0 0 256 256"
      xmlns="http://www.w3.org/2000/svg"
    >
      <defs>
        <linearGradient id={gradientId} x1="0" x2="1" y1="0" y2="1">
          <stop offset="0" stopColor="#32a7fd" />
          <stop offset="1" stopColor="#7b48fc" />
        </linearGradient>
      </defs>
      <path d={sparkle} fill={`url(#${gradientId})`} opacity="0.2" />
      <path
        d={sparkle}
        fill="none"
        stroke={`url(#${gradientId})`}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="16"
      />
    </svg>
  );
}
