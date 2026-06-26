import { Composition } from "remotion";
import { GaussVideo } from "./GaussVideo";
import timeline from "./timeline.json";
import "./font";

export const RemotionRoot: React.FC = () => {
  return (
    <Composition
      id="GaussVideo"
      component={GaussVideo}
      durationInFrames={timeline.durationFrames}
      fps={timeline.fps}
      width={timeline.width}
      height={timeline.height}
    />
  );
};
