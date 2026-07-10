import { type CSSProperties, type FC } from "react";

// Homepage tagline. Each glyph is its own span so it can blur->clear in a
// staggered entrance (synced to CocolaWordmark 0.45s). A second identical
// layer (.cocola-tagline-shine) carries a transparent gradient with one bright
// band, swept left->right periodically for a subtle sheen. The base layer
// stays solid gray so the text is always visible (never disappears). Shared by
// the workspace welcome screen and the login page.
export const TAGLINE_TEXT = "Your trusty & powerful agent platform";

const renderTaglineChars = () => {
  let letterIndex = 0;
  return Array.from(TAGLINE_TEXT).map((chr, i) => {
    if (chr === " ") {
      return (
        <span key={i} className="cocola-tag-sp">
          {"\u00A0"}
        </span>
      );
    }
    const style = { ["--i" as string]: String(letterIndex) } as CSSProperties;
    letterIndex += 1;
    return (
      <span key={i} className="cocola-tag-ch" style={style}>
        {chr}
      </span>
    );
  });
};

export const CocolaTagline: FC = () => (
  <p className="cocola-tagline" aria-label={TAGLINE_TEXT}>
    <span className="cocola-tagline-base" aria-hidden="true">
      {renderTaglineChars()}
    </span>
    <span className="cocola-tagline-shine" aria-hidden="true">
      {renderTaglineChars()}
    </span>
  </p>
);
