# 前端与安全定制

Dashboard 提供独立 Vue 3 应用：

- `/admin/*`：Element Plus 管理后台（主题固定 `spa`）
- `/`、`/service`、`/network`：公开站壳，内置主题白名单切换

## 公开站内置主题

| ID | 说明 |
| -- | ---- |
| `server-status` | 默认公开主题：令牌化顶栏 + 分组表格，点行开右侧只读详情抽屉 |
| `nazhua` | Nazhua 忠实移植：独立页面壳、地图、卡片/列表/ServerStatus、详情地球仪、周期流量 |

Admin「设置 → 外观」可选择默认主题，并可开关「允许访客切换主题」。访客切换写入 `localStorage`（`santaizi-public-theme`），在允许时覆盖站点默认。

公开站仍共用 Santaizi V2 API、WebSocket 和状态 store，但每个主题通过内部 `PublicThemeDefinition` 分别注册 Shell、首页、详情、服务状态和网络页面。同一时刻仅挂载一个 Shell，因此主题头部、背景、主体和页脚不会与另一主题叠加。Nazhua 的功能菜单集中提供主题、语言、明暗模式、服务状态、网络和后台入口。

ServerStatus 使用 `@santaizi/design` 令牌（背景、表面、边框、状态色），站点 `primarycolor` 只驱动指标条等强调色，不改写管理后台的 `--sz-primary` 派生色。不再使用粒子背景或金色 accent。卡片国家/地区优先显示可读 `locationLabel`，否则把位置码（如 `HKG`）和 `CountryCode` 归一为当前语言的短地名与合法 ISO2 旗帜，不把裸码当芯片；世界地图按 geojson `iso_a2` 上色（香港/澳门/台湾并入中国热力）。页脚展示可选的 `footertext`，以及固定品牌名「三太子监控」（链接到面板仓库）与规范化版本号（bootstrap `version`）；上游哪吒版权只出现在 README / LICENSE / NOTICE，不出现在产品面。空页脚文案不再用站点名顶替。

Nazhua 的视觉和资产固定参考上游 commit `d08c973bb4446a24356f49b81d75d6773286596e`，并以在线原版同视口截图、DOM 尺寸和计算样式复核；来源和 MIT 许可见 `NOTICE` 与 `web/packages/theme-nazhua/LICENSE`。主题使用系统中文字体栈，不内置 Sarasa 字体文件。视觉取证、并排比较和可接受差异记录在 `design-qa.md`。

不支持：用户上传主题包、Go HTML 模板主题、任意 `config.js`、对 Nezha v0/v1 的兼容层。

## 安全外观定制

可在管理后台的“设置 → 外观定制”修改品牌色、页脚、Logo 和受限 CSS：

```yaml
site:
  theme: server-status # 或 nazhua
  primarycolor: "#2563eb"
  footertext: "Santaizi Monitoring"
  logourl: "/static/logo.svg"
  safecustomcss: ""
```

Logo 只接受 `/static/` 本地资源或 `data:image/` 图片。CSS 拒绝 `@import`、远程 `url()`、`expression`、`javascript:` 和可执行标签。自定义 HTML/JavaScript 不会执行。

## 嵌入与外置交付

默认在执行 `pnpm build` 后将三个应用嵌入 Go 二进制：

```yaml
web:
  delivery: embedded
```

外置模式必须使用同域反向代理托管静态产物，并继续把 `/api`、`/oauth2`、`/ws` 和 `/openapi` 转发到 Go。
