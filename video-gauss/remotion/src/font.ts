import { staticFile, delayRender, continueRender, cancelRender } from "remotion";

export const FONT_FAMILY = "Source Han Sans SC";

// Load the Heavy CJK font only in the rendering browser. Guarding on `window`
// keeps server-side module evaluation (Getting composition) from touching the
// FontFace/delayRender browser APIs. The ~17MB file needs a long timeout.
if (typeof window !== "undefined" && typeof document !== "undefined") {
  const handle = delayRender("loading-cjk-font", { timeoutInMilliseconds: 120000 });
  const font = new FontFace(
    FONT_FAMILY,
    `url('${staticFile("fonts/SourceHanSansSC-Heavy.otf")}') format('opentype')`,
    { weight: "900" }
  );
  font
    .load()
    .then((loaded) => {
      document.fonts.add(loaded);
      continueRender(handle);
    })
    .catch((e) => cancelRender(e));
}
