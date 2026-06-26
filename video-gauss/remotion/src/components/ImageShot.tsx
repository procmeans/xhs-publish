import { AbsoluteFill, Img, staticFile, interpolate, useCurrentFrame } from "remotion";
import { THEME } from "../theme";

// Illustration scene: slow Ken Burns push-in + bottom gradient to protect captions.
export const ImageShot: React.FC<{ src: string; durationInFrames: number }> = ({
  src,
  durationInFrames,
}) => {
  const frame = useCurrentFrame();
  const scale = interpolate(frame, [0, durationInFrames], [1.06, 1.16], {
    extrapolateRight: "clamp",
  });
  const opacity = interpolate(frame, [0, 12], [0, 1], { extrapolateRight: "clamp" });

  return (
    <AbsoluteFill style={{ backgroundColor: THEME.navyDeep, opacity }}>
      <AbsoluteFill style={{ transform: `scale(${scale})` }}>
        <Img
          src={staticFile(src)}
          style={{ width: "100%", height: "100%", objectFit: "cover" }}
        />
      </AbsoluteFill>
      <AbsoluteFill
        style={{
          background:
            "linear-gradient(to bottom, rgba(9,18,48,0) 55%, rgba(9,18,48,0.55) 78%, rgba(9,18,48,0.85) 100%)",
        }}
      />
    </AbsoluteFill>
  );
};
