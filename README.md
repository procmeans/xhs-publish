# xhs-publish — 小红书自动发布 (Go + Playwright)

照搬《Playwright MCP 实现小红书全自动发布》的核心思路，用 Go 实现：
**只手动登录一次 Chrome，之后通过 CDP 复用登录态自动发布图文 / 视频笔记。**

不脚本化登录（避免滑块/验证码），而是 attach 到一个你已经登录好的 Chrome。

## 工作原理

```
你启动带调试端口的 Chrome ──扫码登录一次──> 保持运行
                                              │
xhspublish ──ConnectOverCDP(:9222)──────────> 复用这个已登录会话
                                              │
          导航创作中心 → 上传图片/视频 → 填标题/正文/话题 → 点发布
```

**始终是真实可见的 Chrome，绝不用无头模式**——发布器只是 attach 到你 `start-chrome.sh`
启动的那个真实窗口操作，启动时会打印 `attaching to real Chrome ... (not headless)`。

## 安装

```bash
go build -o bin/xhspublish ./cmd/xhspublish
# 首次运行需安装 Playwright 驱动（无需下载浏览器，我们 attach 系统 Chrome）：
go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --no-shell || true
```

## 使用

**1. 启动并登录一次 Chrome（保持窗口开着）**

```bash
./scripts/start-chrome.sh
# 在弹出的窗口里扫码登录小红书创作中心
```

**2. 准备一个 task JSON**（见 `examples/task.json`）

```json
{
  "kind": "image",
  "title": "标题不超过20字",
  "content": "正文，不超过1000字",
  "images": ["/绝对路径/1.jpg", "/绝对路径/2.jpg"],
  "topics": ["美食", "家常菜"]
}
```

视频笔记用 `"kind": "video"` + `"video": "/绝对路径/final.mp4"`。

> **封面**：小红书的封面编辑器是会弹原生文件框的模态，不便稳定自动化。可靠做法是把
> 设计好的封面**烧成视频首帧**（小红书默认「截取第一帧」即用它当封面，也兼作标题卡）：
> ```bash
> ffmpeg -y -loop 1 -t 1.0 -i cover.png -i final.mp4 -f lavfi -t 1.0 -i anullsrc=r=48000:cl=stereo \
>   -filter_complex "[0:v]scale=1080:1440:force_original_aspect_ratio=increase,crop=1080:1440,setsar=1,fps=30,format=yuv420p[cv];[1:v]fps=30,format=yuv420p[mv];[cv][mv]concat=n=2:v=1:a=0[v];[2:a][1:a]concat=n=2:v=0:a=1[a]" \
>   -map "[v]" -map "[a]" -c:v libx264 -crf 18 -preset medium -c:a aac -b:a 192k final_with_cover.mp4
> ```

**3. 先 dry-run（默认）——只填表，不点发布，让你肉眼检查**

```bash
./bin/xhspublish -task examples/task.json
```

**4. 确认无误后真正发布**

```bash
./bin/xhspublish -task examples/task.json -publish
```

### 命令行参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `-task` | （必填） | task JSON 路径 |
| `-platform` | `xhs` | 目标平台：`xhs`（小红书）或 `douyin`（抖音） |
| `-cdp` | `http://localhost:9222` | Chrome 调试端口 |
| `-publish` | `false` | 真正点「发布」；不加则只填表（安全默认） |
| `-no-human` | `false` | 关闭拟人化（更快，但更像机器） |
| `-timeout` | `60s` | 单步超时 |

## 抖音（Douyin）

同一套 Go + CDP 框架已支持抖音创作中心（`creator.douyin.com`，目前**仅视频**）。
公共内核（CDP attach、拟人化、上传）共用，平台差异收敛在
`internal/publisher/{xhs,douyin}.go` 两个 `Platform` 实现里。

```bash
# 1. start-chrome.sh 现在会同时打开小红书 + 抖音登录页，两个都扫码登录一次
./scripts/start-chrome.sh

# 2. dry-run（默认，只填表不发布）
./bin/xhspublish -platform douyin -task examples/task_douyin.json

# 3. 确认无误后真正发布
./bin/xhspublish -platform douyin -task examples/task_douyin.json -publish
```

抖音与小红书共用同一个 `PublishTask` 契约（`kind/title/content/video/cover/topics`），
仅标题上限不同（抖音 ≤30 字，小红书 ≤20 字，按 `-platform` 自动选择）。

> **选择器需实测**：抖音创作中心改版频繁，`douyin.go` 的封面 / 发布按钮选择器以
> 文案匹配为主、结构选择器兜底，属 best-effort。封面失败不阻断发布（回退首帧）。
> DOM 漂移时用 `cmd/xhsdebug`（已支持抖音上传页，dump 含标题输入 + 发布按钮候选）重新定位。

## 拟人化（默认开启）

为了让操作不像机器人节拍器，发布器默认注入真人式行为（全部在
`internal/publisher/human.go`，用 `-no-human` 可关）：

- **思考停顿**：每步之间随机延时，区间不固定
- **鼠标轨迹**：按二次贝塞尔曲线（ease-in-out + 抖动）滑向目标，不瞬移
- **悬停 + 随机落点**：点击前先 hover 一拍，落点在元素内随机偏移
- **变速打字**：逐字输入、间隔随机，句末停更久
- **错别字→改正**：偶尔先打错一个字，停顿「察觉」后退格改正
- **标题重输**：有概率打完标题再「反悔」全选清空重打

> 拟人化会让填表慢不少（一条视频笔记约 2 分钟），这是有意为之。

## 破解记录：发布按钮

小红书的「发布」是一个 `xhs-publish-btn` **自定义 Web 组件 + 闭合 shadow DOM**：
「发布」二字不是普通文本（`querySelector` / `getByText` / `getByRole` 全查不到），
红色来自渐变而非背景色——这是平台的反自动化设计。`clickPublish` 的做法：

1. 轮询组件的 `submit-disabled` 属性，等它变 `false`（视频转码完成）才动手；
2. 定位组件，按位置点右侧那颗红色「发布」（`暂存离开` 在左）。

页面 DOM 变了可用 `cmd/xhsdebug` 重新定位元素。

## 对接创意剪辑 skill

`rainwell-creative-editing` 产出成片后，只要写出一个符合上面 schema 的 `task.json`
（`kind: video` + 成片路径 + 文案 + 话题），即可直接：

```bash
./bin/xhspublish -task /path/to/generated_task.json -publish
```

数据契约都在 `internal/task/task.go` 的 `PublishTask`，两边对齐字段即可。

## 注意事项

- **平台限制**：标题 ≤20 字（抖音 ≤30）、正文 ≤1000 字、图文 1–18 张，已在 `ValidateWith()` 中按平台校验。
- **页面 DOM 会变**：公共流程在 `internal/publisher/publisher.go`，各平台选择器分别在 `xhs.go` / `douyin.go`，改版时改对应文件。
- **大文件上传**：媒体通过原生 CDP `DOM.setFileInputFiles`（浏览器本地读盘）上传，**无 50MB 限制**，高清/GB 级视频可直接发；Playwright `SetInputFiles`（CDP 线传，封顶 50MB）仅作兜底。
- **合规**：自动化发布需遵守小红书平台规则，控制频率、避免营销违规词（可配合
  `rainwell-creative-editing` 的 content lint），账号风险自负。
- `xhspublish` 关闭时只断开 CDP 连接，**不会**关掉你的 Chrome。

## 结构

```
cmd/xhspublish/main.go           CLI 入口（-platform xhs|douyin）
cmd/xhsdebug/main.go             元素定位调试工具（DOM 改版时复用，含抖音）
internal/task/task.go            PublishTask 模型 + 按平台校验（数据契约）
internal/publisher/publisher.go  公共内核：CDP attach / 上传 / Platform 接口编排
internal/publisher/xhs.go        小红书 Platform 实现（含 xhs-publish-btn 破解）
internal/publisher/douyin.go     抖音 Platform 实现（视频，best-effort 选择器）
internal/publisher/human.go      拟人化：停顿 / 鼠标轨迹 / 变速打字 / 错别字
scripts/start-chrome.sh          启动可调试 Chrome（同开小红书 + 抖音登录页）
examples/task.json               小红书示例任务
examples/task_douyin.json        抖音示例任务
```

---

# English

**xhs-publish** auto-posts image and video notes to Xiaohongshu (RED) by reusing
a login you do **once**. Following the "Playwright MCP auto-publish" approach, it
never scripts the login (which triggers captchas); instead it attaches over the
Chrome DevTools Protocol to a browser you already logged into.

## How it works

```
You start Chrome with a debug port ──scan-login once──> keep it running
                                                          │
xhspublish ──ConnectOverCDP(:9222)──────────────────────> reuse that session
                                                          │
        creator center → upload media → fill title/body/topics → click 发布
```

It always drives your **real, visible Chrome — never headless**. On start it
prints `attaching to real Chrome ... (not headless)`.

## Install

```bash
go build -o bin/xhspublish ./cmd/xhspublish
# First run installs the Playwright driver (no browser download — we attach your Chrome):
go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --no-shell || true
```

## Usage

```bash
# 1. start Chrome and log into Xiaohongshu once; keep the window open
./scripts/start-chrome.sh

# 2. write a task JSON (see examples/task.json):
#    {"kind":"image","title":"...","content":"...","images":["/abs/1.jpg"],"topics":["food"]}
#    video note: {"kind":"video","video":"/abs/final.mp4", ...}

# 3. dry-run (default): fills the form but does NOT click 发布
./bin/xhspublish -task examples/task.json

# 4. publish for real
./bin/xhspublish -task examples/task.json -publish
```

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-task` | (required) | path to the task JSON |
| `-cdp` | `http://localhost:9222` | Chrome debug endpoint |
| `-publish` | `false` | actually click 发布 (otherwise fill only) |
| `-no-human` | `false` | disable human-like behavior (faster) |
| `-timeout` | `60s` | per-step timeout |

## Human-like behavior (on by default)

So the run doesn't look like a metronome (all in `internal/publisher/human.go`,
disable with `-no-human`): randomized think-pauses, quadratic-Bézier mouse paths
(ease-in-out + jitter), hover-before-click with a randomized landing point,
variable-speed typing, occasional typos that get backspaced and fixed, and a
chance to clear and retype the title. This makes filling a note take ~2 minutes,
by design.

## Notes

- **Publish button**: it's an `xhs-publish-btn` custom element with a *closed*
  shadow root — its "发布" label is invisible to text/role/CSS selectors and the
  red is a gradient. `clickPublish` polls the host's `submit-disabled` attribute
  (waits for video transcoding) then clicks the right-hand 发布 pill by position.
  Use `cmd/xhsdebug` to re-locate elements if the DOM changes.
- **Cover**: the cover editor opens a native file dialog and is awkward to drive,
  so the reliable path is baking your cover as the video's first frame (ffmpeg
  snippet above) — Xiaohongshu's default "first frame" then uses it.
- **Limits**: title ≤20 chars, body ≤1000, 1–18 images — enforced in `Validate()`.
- **Compliance**: follow Xiaohongshu's rules, throttle frequency, avoid banned
  marketing terms. Automated posting is at your own account's risk.
- Closing `xhspublish` only detaches CDP; it does **not** close your Chrome.
