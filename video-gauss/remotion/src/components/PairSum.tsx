import {
  AbsoluteFill,
  interpolate,
  spring,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { THEME } from "../theme";
import { FONT_FAMILY } from "../font";

// Visualizes Gauss's pairing trick: 1+100, 2+99, ... = 101 each, 50 pairs.
const TOP = [1, 2, 3, "…", 50];
const BOT = [100, 99, 98, "…", 51];

export const PairSum: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const rowIn = (i: number) =>
    spring({ frame: frame - 10 - i * 6, fps, config: { damping: 200 } });

  // Pair arcs + "=101" appear after the rows settle.
  const pairReveal = interpolate(frame, [70, 110], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const eqReveal = spring({ frame: frame - 150, fps, config: { damping: 200 } });
  const resultReveal = spring({ frame: frame - 220, fps, config: { damping: 200 } });

  const cell = (v: number | string, key: string, prog: number) => (
    <div
      key={key}
      style={{
        width: 150,
        height: 150,
        margin: 10,
        borderRadius: 24,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        fontSize: 72,
        fontFamily: FONT_FAMILY,
        color: THEME.navy,
        background: THEME.cream,
        boxShadow: "0 10px 30px rgba(0,0,0,0.25)",
        opacity: prog,
        transform: `translateY(${(1 - prog) * 40}px)`,
      }}
    >
      {v}
    </div>
  );

  return (
    <AbsoluteFill
      style={{
        background: `radial-gradient(circle at 50% 35%, ${THEME.navy}, ${THEME.navyDeep})`,
        alignItems: "center",
        justifyContent: "center",
        flexDirection: "column",
      }}
    >
      <div style={{ display: "flex" }}>
        {TOP.map((v, i) => cell(v, "t" + i, rowIn(i)))}
      </div>

      <div
        style={{
          height: 70,
          display: "flex",
          alignItems: "center",
          gap: 40,
          opacity: pairReveal,
        }}
      >
        {[0, 1, 2, 3, 4].map((i) => (
          <div
            key={i}
            style={{
              width: 150,
              textAlign: "center",
              color: THEME.amber,
              fontSize: 46,
              fontFamily: FONT_FAMILY,
            }}
          >
            ↕
          </div>
        ))}
      </div>

      <div style={{ display: "flex" }}>
        {BOT.map((v, i) => cell(v, "b" + i, rowIn(i)))}
      </div>

      <div
        style={{
          marginTop: 50,
          fontSize: 64,
          fontFamily: FONT_FAMILY,
          color: THEME.textOnDark,
          opacity: eqReveal,
          transform: `scale(${0.85 + eqReveal * 0.15})`,
        }}
      >
        每一对都 = <span style={{ color: THEME.amber }}>101</span>
      </div>

      <div
        style={{
          marginTop: 28,
          fontSize: 84,
          fontWeight: 900,
          fontFamily: FONT_FAMILY,
          color: THEME.amber,
          opacity: resultReveal,
          transform: `scale(${0.8 + resultReveal * 0.2})`,
          textShadow: "0 6px 24px rgba(232,161,58,0.5)",
        }}
      >
        50 × 101 = 5050
      </div>
    </AbsoluteFill>
  );
};
