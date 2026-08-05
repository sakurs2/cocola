"use client";

import { BrainCircuit } from "lucide-react";
import Image from "next/image";
import { useEffect, useState, type FC } from "react";
import {
  LOCAL_SIMPLE_ICON_PATHS,
  SIMPLE_ICON_FALLBACK_BADGES,
  lobeIconPath,
  normalizeLobeIconSlug,
  type ModelIconConfig,
} from "@/lib/model-icons";
import { cn } from "@/lib/utils";

export const ModelIcon: FC<{
  icon?: ModelIconConfig;
  className?: string;
  bare?: boolean;
}> = ({ icon, className, bare = false }) => {
  const [lobeFailed, setLobeFailed] = useState(false);
  const normalizedSlug = normalizeLobeIconSlug(icon?.slug);
  const canUseLobeIcon =
    (icon?.type === "lobe-icons" || icon?.type === "simple-icons") && normalizedSlug !== "";
  const lobePath = canUseLobeIcon && !lobeFailed ? lobeIconPath(normalizedSlug) : "";
  const simpleIconPath =
    !lobePath && icon?.type === "simple-icons" && icon.slug
      ? LOCAL_SIMPLE_ICON_PATHS[icon.slug.toLowerCase()]
      : "";

  useEffect(() => {
    setLobeFailed(false);
  }, [icon?.slug, icon?.src, icon?.type]);

  // Composer pills render the native logo without a second frame.
  const frame = (tone: string) =>
    cn(
      "flex shrink-0 items-center justify-center overflow-hidden",
      bare ? "" : cn("rounded-full border border-border", tone),
      className,
    );
  const imageSize = bare ? "size-full object-contain" : "size-[72%] object-contain";

  if (icon?.type === "image" && icon.src) {
    return (
      <span className={cn(frame("bg-surface"), "relative")}>
        <Image
          src={icon.src}
          alt=""
          width={256}
          height={256}
          unoptimized
          className="size-full object-contain"
          aria-hidden="true"
        />
      </span>
    );
  }
  if (lobePath) {
    return (
      <span className={frame("bg-white")} aria-hidden="true">
        <Image
          src={lobePath}
          alt=""
          width={96}
          height={96}
          unoptimized
          className={imageSize}
          onError={() => setLobeFailed(true)}
        />
      </span>
    );
  }
  if (simpleIconPath) {
    return (
      <span className={frame("bg-white")} aria-hidden="true">
        <Image
          src={simpleIconPath}
          alt=""
          width={96}
          height={96}
          unoptimized
          className={imageSize}
        />
      </span>
    );
  }

  const fallbackBadge =
    (icon?.type === "simple-icons" || icon?.type === "lobe-icons") && icon.slug
      ? SIMPLE_ICON_FALLBACK_BADGES[icon.slug.toLowerCase()] ||
        SIMPLE_ICON_FALLBACK_BADGES[normalizedSlug]
      : "";
  if (!fallbackBadge) {
    return (
      <span className={cn(frame("bg-background"), "text-muted")}>
        <BrainCircuit className={bare ? "size-full" : "size-[70%]"} />
      </span>
    );
  }
  return (
    <span
      className={cn(frame("bg-surface-secondary"), "text-[9px] font-bold leading-none text-foreground")}
      aria-hidden="true"
    >
      {fallbackBadge}
    </span>
  );
};
