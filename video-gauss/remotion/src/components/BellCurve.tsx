import {
  AbsoluteFill,
  interpolate,
  useCurrentFrame,
  useVideoConfig,
  spring,
} from "remotion";
import { THEME } from "../theme";
import { FONT_FAMILY } from "../font";

// Exact standard-normal curve: y = exp(-x^2 / 2). Drawn point-by-point (no
// hand-drawn approximation), per the explainer data-accuracy rule.
const W = 980;
const H = 560;
const X_MIN = -3.4;
const X_MAX = 3.4;

const gauss = (x: number) => Math.exp(-(x * x) / 2);

const toPx = (x: number, y: number) => {
  const px = interpolate(x, [X_MIN, X_MAX], [0, W]);
  const py = interpolate(y, [0, 1], [H, 40]);
  return [px, py] as const;
};

const buildPath = (frac: number) => {
  const xEnd = interpolate(frac, [0, 1], [X_MIN, X_MAX]);
  let d = "";
  for (let i = 0; i <= 240; i++) {
    const x = X_MIN + ((xEnd - X_MIN) * i) / 240;
    const [px, py] = toPx(x, gauss(x));
    d += `${i === 0 ? "M" : "L"}${px.toFixed(1)},${py.toFixed(1)} `;
  }
  return d;
};

// Shaded ±1σ region (≈68.27%).
const buildSigmaArea = () => {
  let d = `M${toPx(-1, 0)[0]},${toPx(-1, 0)[1]} `;
  for (let i = 0; i <= 120; i++) {
    const x = -1 + (2 * i) / 120;
    const [px, py] = toPx(x, gauss(x));
    d += `L${px.toFixed(1)},${py.toFixed(1)} `;
  }
  d += `L${toPx(1, 0)[0]},${toPx(1, 0)[1]} Z`;
  return d;
};

export const BellCurve: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const draw = interpolate(frame, [10, 110], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const shade = interpolate(frame, [150, 200], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const labelIn = spring({ frame: frame - 210, fps, config: { damping: 200 } });

  // Light point following the curve as it draws.
  const headX = interpolate(draw, [0, 1], [X_MIN, X_MAX]);
  const [hx, hy] = toPx(headX, gauss(headX));

  return (
    <AbsoluteFill
      style={{
        background: `radial-gradient(circle at 50% 40%, ${THEME.navy}, ${THEME.navyDeep})`,
        alignItems: "center",
        justifyContent: "center",
        flexDirection: "column",
      }}
    >
      <svg width={W} height={H + 30} viewBox={`0 0 ${W} ${H + 30}`}>
        <defs>
          <linearGradient id="bell" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={THEME.amber} stopOpacity="0.55" />
            <stop offset="100%" stopColor={THEME.amber} stopOpacity="0.05" />
          </linearGradient>
        </defs>

        {/* baseline */}
        <line x1={0} y1={H} x2={W} y2={H} stroke={THEME.cream} strokeOpacity={0.35} strokeWidth={2} />

        {/* ±1σ shaded ~68% */}
        <path d={buildSigmaArea()} fill="url(#bell)" opacity={shade} />

        {/* mean line */}
        <line
          x1={toPx(0, 0)[0]}
          y1={H}
          x2={toPx(0, 0)[0]}
          y2={toPx(0, 1)[1]}
          stroke={THEME.cream}
          strokeOpacity={0.3}
          strokeDasharray="6 8"
          strokeWidth={2}
        />

        {/* curve */}
        <path
          d={buildPath(draw)}
          fill="none"
          stroke={THEME.amber}
          strokeWidth={7}
          strokeLinecap="round"
          style={{ filter: "drop-shadow(0 0 12px rgba(244,201,93,0.7))" }}
        />

        {draw < 1 && <circle cx={hx} cy={hy} r={11} fill={THEME.cream} />}

        {/* sigma ticks */}
        {[-1, 1].map((s) => (
          <line
            key={s}
            x1={toPx(s, 0)[0]}
            y1={H - 10}
            x2={toPx(s, 0)[0]}
            y2={H + 10}
            stroke={THEME.cream}
            strokeOpacity={0.6}
            strokeWidth={3}
          />
        ))}
      </svg>

      <div
        style={{
          marginTop: 10,
          fontSize: 60,
          fontFamily: FONT_FAMILY,
          color: THEME.textOnDark,
          opacity: labelIn,
          transform: `translateY(${(1 - labelIn) * 24}px)`,
        }}
      >
        中间这段（±1σ）≈ <span style={{ color: THEME.amber }}>68%</span>
      </div>
    </AbsoluteFill>
  );
};
