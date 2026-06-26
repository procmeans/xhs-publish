import {
  AbsoluteFill,
  Audio,
  Sequence,
  staticFile,
  interpolate,
  useCurrentFrame,
  useVideoConfig,
  spring,
} from "remotion";
import { THEME } from "./theme";
import { FONT_FAMILY } from "./font";
import { ImageShot } from "./components/ImageShot";
import { PairSum } from "./components/PairSum";
import { BellCurve } from "./components/BellCurve";
import { CaptionTrack } from "./components/CaptionTrack";
import timeline from "./timeline.json";

type Scene = {
  id: number;
  visual: string;
  startFrame: number;
  endFrame: number;
};

const renderVisual = (visual: string, dur: number) => {
  if (visual.startsWith("img:")) {
    return <ImageShot src={`img/${visual.slice(4)}`} durationInFrames={dur} />;
  }
  if (visual === "anim:PairSum") return <PairSum />;
  if (visual === "anim:BellCurve") return <BellCurve />;
  return <AbsoluteFill style={{ background: THEME.navy }} />;
};

const TopBadge: React.FC = () => (
  <AbsoluteFill style={{ alignItems: "center", justifyContent: "flex-start", paddingTop: 70 }}>
    <div
      style={{
        fontFamily: FONT_FAMILY,
        fontWeight: 900,
        fontSize: 38,
        letterSpacing: 4,
        color: THEME.navyDeep,
        background: THEME.amber,
        padding: "10px 28px",
        borderRadius: 999,
        boxShadow: "0 6px 18px rgba(0,0,0,0.3)",
      }}
    >
      数学王子 · 高斯
    </div>
  </AbsoluteFill>
);

const ProgressBar: React.FC = () => {
  const frame = useCurrentFrame();
  const { durationInFrames } = useVideoConfig();
  const w = interpolate(frame, [0, durationInFrames], [0, 100]);
  return (
    <AbsoluteFill style={{ justifyContent: "flex-end" }}>
      <div style={{ height: 8, background: "rgba(255,255,255,0.12)" }}>
        <div style={{ height: 8, width: `${w}%`, background: THEME.amber }} />
      </div>
    </AbsoluteFill>
  );
};

export const GaussVideo: React.FC = () => {
  const { fps } = useVideoConfig();
  const frame = useCurrentFrame();
  const scenes = timeline.scenes as Scene[];

  // End card fades in over the last scene's tail.
  const last = scenes[scenes.length - 1];
  const endIn = spring({ frame: frame - (last.endFrame - 70), fps, config: { damping: 200 } });

  return (
    <AbsoluteFill style={{ backgroundColor: THEME.navyDeep }}>
      <Audio src={staticFile("voiceover.wav")} />

      {scenes.map((s) => {
        const dur = s.endFrame - s.startFrame;
        return (
          <Sequence key={s.id} from={s.startFrame} durationInFrames={dur}>
            {renderVisual(s.visual, dur)}
          </Sequence>
        );
      })}

      <TopBadge />
      <CaptionTrack />
      <ProgressBar />

      {endIn > 0.01 && (
        <AbsoluteFill
          style={{
            background: `radial-gradient(circle at 50% 45%, ${THEME.navy}, ${THEME.navyDeep})`,
            alignItems: "center",
            justifyContent: "center",
            flexDirection: "column",
            opacity: endIn,
          }}
        >
          <div style={{ fontFamily: FONT_FAMILY, fontSize: 88, fontWeight: 900, color: THEME.amber }}>
            数学，是科学的女王
          </div>
          <div
            style={{
              marginTop: 30,
              fontFamily: FONT_FAMILY,
              fontSize: 46,
              color: THEME.textOnDark,
            }}
          >
            关注我，看更多数学家的故事 👑
          </div>
        </AbsoluteFill>
      )}
    </AbsoluteFill>
  );
};
