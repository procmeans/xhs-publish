import { AbsoluteFill, interpolate, useCurrentFrame } from "remotion";
import { THEME } from "../theme";
import { FONT_FAMILY } from "../font";
import timeline from "../timeline.json";

type Scene = {
  startFrame: number;
  endFrame: number;
  captions: string[];
};

// Global caption track: reads absolute frame against the timeline so captions
// stay in sync regardless of which visual (image vs animation) is on screen.
export const CaptionTrack: React.FC = () => {
  const frame = useCurrentFrame();
  const scenes = timeline.scenes as Scene[];
  const scene = scenes.find((s) => frame >= s.startFrame && frame < s.endFrame);
  if (!scene) return null;

  const n = scene.captions.length;
  const span = (scene.endFrame - scene.startFrame) / n;
  const idx = Math.min(n - 1, Math.floor((frame - scene.startFrame) / span));
  const text = scene.captions[idx];

  const localStart = scene.startFrame + idx * span;
  const t = frame - localStart;
  const appear = interpolate(t, [0, 8], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill
      style={{ justifyContent: "flex-end", alignItems: "center", paddingBottom: 150 }}
    >
      <div
        style={{
          maxWidth: 920,
          textAlign: "center",
          fontFamily: FONT_FAMILY,
          fontWeight: 900,
          fontSize: 66,
          lineHeight: 1.3,
          color: THEME.textOnDark,
          opacity: appear,
          transform: `translateY(${(1 - appear) * 18}px)`,
          padding: "18px 36px",
          textShadow:
            "0 3px 0 rgba(0,0,0,0.55), 0 0 22px rgba(0,0,0,0.6), 4px 4px 8px rgba(0,0,0,0.7)",
          WebkitTextStroke: `2px ${THEME.navyDeep}`,
        }}
      >
        {text}
      </div>
    </AbsoluteFill>
  );
};
