# Kintsugi

macOS 菜单栏 app：在任何地方选中自己写的英文，按 `⌥⌘G` 纠错；每处错误都结构化入库，统计出个人高频错误。名字取自金继：错误不删掉，留下来变成进步的痕迹。

当前状态：**第 0 步验证工具 + 阶段一（MVP）**。复习、趋势（阶段二）和主动提升（阶段三）还没做。

## 安装

从 [Releases](https://github.com/youngle316/Kintsugi/releases/latest) 下载 `kintsugi-<版本>.dmg`（Apple 芯片和 Intel 通用），把 Kintsugi 拖进“应用程序”。

Kintsugi 用免费的 Apple Development 证书签名，没有公证，所以第一次打开时 macOS 会拦下（“无法验证开发者”）。处理一次即可：

- 系统设置 ▸ 隐私与安全性，往下找到 Kintsugi 那条提示，点 **仍要打开**；或者
- 终端里执行 `xattr -dr com.apple.quarantine /Applications/Kintsugi.app`，再打开。

之后按首次启动的引导授权辅助功能，填写 SiliconFlow API Key。

**更新**：Kintsugi 启动 10 秒后、之后每天检查一次 GitHub Releases，有新版本就弹窗显示更新说明，可以安装、稍后提醒或跳过。也可以随时用菜单栏 ▸ **Check for Updates…**，或 设置 ▸ About ▸ **Check for Updates…**。更新包用发布密钥签名，app 只接受签名正确的包；每个版本都用同一张证书签名，所以**更新后辅助功能授权不会丢**。

## 功能

- **纠错浮窗**：`⌥⌘G` 读取前台 app 的选中文字（没选中就读当前输入框全文），浮窗显示修改后的全文、每处错误的高亮、类型、级别和中/英文解释。浮窗不抢前台，原 app 保持激活。
  - **替换**（`⌘↩`）写回原处，**复制**，**忽略**（`Esc`）；无论哪个操作都入库。没有错误显示 “Looks good”，也记一次无错文本。
- **主窗口**：概览（检测数、词数、错误率 = error 数 / 词数 × 100、类型分布图、近 30 天 Top 10 错误模式、来源 app）、错题本（按类型、级别、来源 app、时间、模式筛选）、Capture Lab（第 0 步）、设置。
- **设置**：SiliconFlow API Key（存钥匙串）、API 地址、纠错模型和高质量模型、解释语言、快捷键、开机启动、辅助功能授权、按 app 的采集模式。
- **首次启动**：引导开启辅助功能权限（MyGo 的 `permissionflow`，会打开系统设置并在下方给出可拖拽的 app 卡片），填写 API Key。

## 技术栈

| 部分 | 实现 |
|---|---|
| 框架 | [MyGo](https://github.com/egoist/mygo) **v0.10.0**（锁定：`go.mod` 的 `github.com/egoist/mygo v0.10.0`，`package.json` 里 `mygo-cli`、`mygo-runtime` 精确为 `0.10.0`；CLI 用 `go tool mygo`，同一版本）。升级前先读 release notes：Native Module 是实验性功能。 |
| 后台 app | `LSUIElement` + `ActivationPolicyAccessory`，托盘图标，单实例 |
| 采集 | Swift Native Module [`modules/capture`](modules/capture)：`IsTrusted`、`FrontmostApp`、`GetSelectedText`、`ReplaceSelection`，以及诊断用的 `InspectFocus` |
| 浮窗 | MyGo Panel（非激活 `NSPanel`），页面 `/panel` |
| 数据 | MyGo `sqlite` 插件，`~/Library/Application Support/Kintsugi/kintsugi.db`（开发版在 `Kintsugi Dev` 目录） |
| 密钥 | `mygo.Secrets`（登录钥匙串，service = app identifier，account = `siliconflow-api-key`） |
| LLM | SiliconFlow OpenAI 兼容接口 [`internal/llm`](internal/llm) |
| 前端 | TypeScript + Vite，图表用 Chart.js |

### 采集策略（`modules/capture`）

读取，按设置里该 app 的模式：

1. **auto**（默认）：辅助功能接口读 `AXSelectedText`。读之前给目标 app 设 `AXManualAccessibility`，让 Electron / Chromium 构建辅助功能树。
2. 读不到就模拟 `⌘C`，等剪贴板变化后读出，**然后把剪贴板原样恢复**（所有 item 的所有类型）。
3. 没有选中内容：AX 读 `AXValue` 全文；或者模拟 `⌘A ⌘C`。终端（Terminal、iTerm2、Ghostty、Warp…）不做全文读取，因为 `⌘A` 会选中整个回滚缓冲区。

写回：

1. auto 模式先用 AX 设置 `AXSelectedText`（全文时是 `AXValue`），再读回 `AXValue` **确认确实变了**；读不回就不用 AX，避免和粘贴重复插入。
2. 否则把原 app 拉回前台，模拟 `⌘V`（全文时先 `⌘A`），450 ms 后恢复剪贴板；临时内容带 `org.nspasteboard.TransientType` 标记，剪贴板管理器不会记录。
3. 都失败时，修正结果留在剪贴板上，浮窗提示手动粘贴。

**clipboard** 模式跳过 AX，只用 `⌘C` / `⌘V`。**manual** 模式是兜底的兜底：你自己 `⌘C`，按快捷键读剪贴板，“替换”变成把结果放回剪贴板，不需要辅助功能权限。

### LLM（`internal/llm`）

- 纠错 prompt 基于需求文档第 8 节，同时要求中英文两种解释（`explanation_zh`、`explanation_en`），切换语言时不用重新请求。
- 发送前**预先剥离代码**：代码块、反引号、URL、路径都替换成 `⟦C1⟧` 这样的占位符，要求模型原样保留，回来后再还原。模型丢了占位符就算失败并重试。
- 带 `response_format: json_object` 和 `enable_thinking: false` 请求；模型返回 400（不支持这两个参数）时，去掉它们只靠 prompt 再请求。JSON 无效时重试一次。
- `category` 归一到 14 个固定类型，`pattern` 归一（小写、合并空格、统一箭头为 ` -> `）后聚合。
- 词数不计代码；错误率只算 `severity = error`。

## 发布

推送 `v*` tag 时，[`.github/workflows/release.yml`](.github/workflows/release.yml) 在 macOS runner 上构建通用版（`darwin/universal`），用证书签名，用更新私钥签更新包，上传 DMG、更新包和清单，然后**直接发布**（不留草稿），已安装的 app 随即能更新到它。

### 一次性配置

1. **更新密钥**（只在本机生成，私钥不进仓库）：

   ```sh
   go tool mygo keygen            # 打印 mygo-update.key 和 mygo-update.pub 的位置
   ```

   把 `mygo-update.pub` 的内容填进 `mygo.config.ts` 的 `UPDATE_PUBLIC_KEY`。私钥丢了，已安装的 app 就再也收不到更新，所以放进密码管理器备份。

2. **签名身份**：用 youngle316@gmail.com 的 Personal Team 生成的 Apple Development 证书（Xcode ▸ Settings ▸ Accounts ▸ Manage Certificates）。**不要用 Bared Monkey Inc. 团队的证书**。用 `security find-identity -v -p codesigning` 找到完整名称，填进 `SIGNING_IDENTITY`，形如 `Apple Development: 名字 (XXXXXXXXXX)`。填了之后，本机 `bun run build` 也用它签名；`bun run dev` 会挑这个团队的开发证书签开发版。

3. **仓库 secrets**（钥匙串访问 ▸ 我的证书，右键该证书 ▸ 导出，存成带密码的 `.p12`）：

   ```sh
   base64 -i certificate.p12 | gh secret set MACOS_CERTIFICATE -R youngle316/Kintsugi
   gh secret set MACOS_CERTIFICATE_PASSWORD -R youngle316/Kintsugi          # .p12 的密码
   gh secret set MYGO_UPDATER_PRIVATE_KEY -R youngle316/Kintsugi < path/to/mygo-update.key
   ```

4. 仓库要是**公开**的：app 从 `https://github.com/youngle316/Kintsugi/releases/latest/download/` 读更新清单，私有仓库读不到。

在这两个值填好之前，构建是 ad hoc 签名、不带更新，工作流会在第一步报错退出。

### 发一个版本

1. 改 `package.json` 的 `version`（唯一的版本号来源，`mygo.config.ts` 读它）。
2. 在 `CHANGELOG.md` 加 `## <版本>` 一节：它就是更新窗口和 GitHub release 的说明，缺了工作流会报错。
3. 提交、推送，然后打 tag：

   ```sh
   git tag v0.2.0 && git push origin v0.2.0
   ```

工作流会检查 tag 和 `package.json` 版本一致、CHANGELOG 有这一节、两个值已填、导入的证书正好是 `SIGNING_IDENTITY`，最后验证 app 的签名。

### 证书续期

Apple Development 证书有效期一年。到期前在 Xcode 里重新生成一张（同一个 Personal Team，名称不变），导出新的 `.p12` 更新 `MACOS_CERTIFICATE` 和密码即可。名称不变时 macOS 通常会保留辅助功能授权；如果更新后提示没有权限，在辅助功能列表里删掉 Kintsugi 再加一次。

## 本机开发

需要：macOS 13+、Xcode 26 或更新（Swift 6，`xcode-select -p` 能找到）、Go 1.27+、Bun **1.4.4 或更新**（`bun.lock` 是 1.4.4 写的；旧版 Bun 会报 `Unknown lockfile version`，用 `bun upgrade` 升级）。

```sh
git clone https://github.com/youngle316/Kintsugi && cd Kintsugi
bun install
bun run dev        # mygo dev：Vite + 构建 Swift 模块 + 运行 "Kintsugi Dev"，改代码自动重启
```

打包：

```sh
bun run build                                   # build/darwin-arm64/Kintsugi.app 和 .dmg
bun run build -- -platform darwin/universal     # 和发布一样的通用版
```

测试：

```sh
go test ./...                              # LLM 解析/掩码/重试、SQLite 入库与统计
go tool mygo native check modules/capture  # 生成并编译 Swift 模块，列出问题
bun run typecheck
```

开发时也可以用环境变量 `SILICONFLOW_API_KEY` 提供 key（仅在钥匙串里没有 key 时生效），方便测试。

### 签名（重要）

macOS 按签名记住辅助功能授权和钥匙串的“始终允许”。ad hoc 签名每次构建都被当成新 app，**每次都要重新授权**。配置一次免费的开发证书：

1. Xcode ▸ Settings ▸ Accounts，用 Apple ID 登录（免费账号即可）。
2. 选中账号 ▸ Manage Certificates ▸ `+` ▸ **Apple Development**。
3. 确认：`security find-identity -v -p codesigning` 能看到 `Apple Development: …`。

之后 `bun run dev` 会**自动**用这个证书签开发版（启动时打印 `Signing the development app as Apple Development: …`；没找到会警告 “Signing the development app ad hoc”）。

`bun run build` 用 `mygo.config.ts` 的 `SIGNING_IDENTITY` 签名（没填就 ad hoc）。临时换一个：`KINTSUGI_SIGNING_IDENTITY=- bun run build` 是 ad hoc。

### 授权辅助功能

- 开发版叫 **Kintsugi Dev**（identifier `com.youngle316.kintsugi.dev`），和正式版 **Kintsugi**（`com.youngle316.kintsugi`）是两个 app，权限、钥匙串条目、数据各自独立，需要分别授权。
- 首次启动的引导页，或 设置 ▸ Accessibility ▸ **Grant Accessibility…**，会打开 系统设置 ▸ 隐私与安全性 ▸ 辅助功能，并在窗口下方显示 app 卡片，拖进列表、打开开关即可。
- 授权后通常立即生效；如果 Kintsugi 仍提示没权限，退出重开一次。
- 如果签名换过（比如从 ad hoc 改成证书），列表里旧的条目会失效：用 `-` 删掉旧条目再重新添加。
- 用终端直接运行可执行文件（`go run`）时，权限算在终端头上，以 app 方式运行才算 Kintsugi 自己的。

## 正式版和开发版

| | 正式版 Kintsugi | 开发版 Kintsugi Dev（`bun run dev`） |
|---|---|---|
| Bundle ID | `com.youngle316.kintsugi` | `com.youngle316.kintsugi.dev` |
| 数据目录 | `~/Library/Application Support/Kintsugi/` | `~/Library/Application Support/Kintsugi Dev/` |
| API Key | 钥匙串，service `com.youngle316.kintsugi` | 钥匙串，service `com.youngle316.kintsugi.dev` |
| 辅助功能授权 | 单独一条 | 单独一条 |

两者完全分开：各自授权辅助功能、各自填 API Key，数据互不影响，可以同时装着。两个同时运行时快捷键会冲突（后启动的那个注册不上 `⌥⌘G`），测试时退出其中一个。

**把开发版的数据带到正式版**：数据目录里是 `kintsugi.db`（错题和检测记录，SQLite，可能带 `-wal`、`-shm`）、`settings.json`（模型、语言、快捷键、按 app 的采集模式）和 `capture-lab.json`。先**退出两个 app**（菜单栏 ▸ Quit），然后：

```sh
src="$HOME/Library/Application Support/Kintsugi Dev"
dst="$HOME/Library/Application Support/Kintsugi"
mkdir -p "$dst"
cp "$src"/kintsugi.db* "$src"/settings.json "$src"/capture-lab.json "$dst"/ 2>/dev/null
```

这会覆盖正式版已有的数据。API Key 在钥匙串里，不会跟着过去，在正式版里重新填一次。

## 第 0 步：采集可行性验证

在你自己的 Mac 上，用真实的 Amp 和 Octarine 测试。工具是菜单栏 ▸ **Capture Lab (step 0)**，快捷键 `⌃⌥⌘L`。

准备：装好正式版并授权（或用 `bun run dev` 运行开发版），打开 Capture Lab 页。结果在对应数据目录的 `capture-lab.json` 里。

在下面每个地方各测一遍：

1. **Amp 桌面端**（或 CLI 所在的终端，Terminal / iTerm2 / Ghostty 分开记）
2. **Amp 网页版**（浏览器，Safari 和 Chrome 分开记）
3. **Octarine**
4. **浏览器里的普通输入框**（对照组，比如任何网页的 textarea）

每个地方：

1. 输入一句英文，**选中一部分**，按 `⌃⌥⌘L`。Lab 会分别用 `⌘C` 和辅助功能接口各读一次，记录成功与否、读到的内容、焦点元素的 AX 角色和属性、每一步的日志。
2. 在 Lab 里点 **Write AX**：用辅助功能接口在选区末尾追加 `[kintsugi ax test]`。回到原 app 看是否出现，在下拉框选 Yes / No。
3. 回原 app 重新选中，再按一次 `⌃⌥⌘L`，点 **Write ⌘V**，同样确认。
4. 在 “What was this?” 选这是哪个场景。
5. 再用主快捷键 `⌥⌘G` 实测一次完整流程：**选中一部分**、**什么都不选（光标在输入框里）** 各一次，看浮窗内容和“替换”结果是否正确，原文是否被正确替换（而不是追加）。

全部测完点 **Copy results as Markdown**，把表格发回来。

根据结果设置每个 app 的模式（设置 ▸ Per-app capture，填 bundle ID，Lab 卡片标题里有）：

| 结果 | 模式 |
|---|---|
| AX 读和写都行 | 不用设，auto |
| AX 不行、`⌘C`/`⌘V` 行 | clipboard（跳过 AX，少一次等待） |
| 都不行，或替换会出错（比如终端里粘贴会插到提示符，而不是替换选区） | manual：自己 `⌘C` 后按快捷键，结果复制回剪贴板 |

需要重点留意的：

- Electron app 的 AX 树是否在设了 `AXManualAccessibility` 后可用（Lab 日志里有这一步的返回值）。
- 终端里：选中的是回滚缓冲区的输出，`⌘V` 会粘贴到提示符而不是替换选区，大概率要设成 manual 或只用“复制”。
- 默认快捷键 `⌥⌘G` 是否和你已有的快捷键冲突（被占用时设置页保存会报错并保留旧的）。

## 云端已验证的部分

在 Mac satellite（macOS 27，Xcode 27.3）上，用 TextEdit 和一个模拟 SiliconFlow 的本地服务实测：

- AX 读选区 → 浮窗显示 → `⌘↩` 替换：AX 写入并读回确认，原文被修正，剪贴板没被动过，submission 记为 `replaced`，两条 issue 入库。
- clipboard 模式、什么都不选：`⌘A ⌘C` 读全文 → `⌘A ⌘V` 替换，**读取后和写回后剪贴板都恢复为原内容**。
- Capture Lab：两种读取、AX 写入都正常记录。
- 单实例：第二次启动直接退出，并打开第一个实例的主窗口。

没验证的：真实 SiliconFlow 模型的输出质量、Amp / Octarine / 浏览器的行为（就是第 0 步）。

## 目录

```
main.go                  启动、快捷键、单实例
kintsugi.go              页面调用的后端：纠错流程、浮窗、托盘、设置、权限
lab.go                   第 0 步 Capture Lab
internal/llm             SiliconFlow 调用、代码掩码、JSON 校验与归一
internal/store           SQLite：submissions、issues、统计
internal/settings        设置文件（API Key 不在这里）
modules/capture          Swift Native Module（capture.go 是接口，swift/ 是实现，*_native.go 和 *.mygo.swift 是生成的）
src/                     前端：app.ts 主窗口，panel.ts 浮窗，mygo.ts 是生成的客户端
.github/workflows        release.yml：推 v* tag 构建、签名、发布
CHANGELOG.md             每个版本的更新说明
```

## 已知限制

- App 图标还是 MyGo 模板的默认图标（`resources/icon.png`，1024×1024 PNG），更新窗口里也显示它。
- 浮窗位置优先用选区的屏幕坐标（`AXBoundsForRange`），拿不到就放在鼠标附近。
- 复习队列、趋势、已掌握判定（阶段二）以及数据表 `review_cards`、`review_logs` 还没建。
- 数据库没有迁移机制，阶段二加表时用 `CREATE TABLE IF NOT EXISTS` 即可。
