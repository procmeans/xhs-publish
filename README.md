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
| `-cdp` | `http://localhost:9222` | Chrome 调试端口 |
| `-publish` | `false` | 真正点「发布」；不加则只填表（安全默认） |
| `-no-human` | `false` | 关闭拟人化（更快，但更像机器） |
| `-timeout` | `60s` | 单步超时 |

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

- **平台限制**：标题 ≤20 字、正文 ≤1000 字、图文 1–18 张，已在 `Validate()` 中校验。
- **页面 DOM 会变**：选择器集中在 `internal/publisher/publisher.go`，小红书改版时改这里。
- **合规**：自动化发布需遵守小红书平台规则，控制频率、避免营销违规词（可配合
  `rainwell-creative-editing` 的 content lint），账号风险自负。
- `xhspublish` 关闭时只断开 CDP 连接，**不会**关掉你的 Chrome。

## 结构

```
cmd/xhspublish/main.go           CLI 入口
cmd/xhsdebug/main.go             元素定位调试工具（DOM 改版时复用）
internal/task/task.go            PublishTask 模型 + 校验（数据契约）
internal/publisher/publisher.go  Playwright/CDP 发布流程（含 xhs-publish-btn 破解）
internal/publisher/human.go      拟人化：停顿 / 鼠标轨迹 / 变速打字 / 错别字
scripts/start-chrome.sh          启动可调试 Chrome
examples/task.json               示例任务
```
