# Kintsugi

Kintsugi 是一款 macOS 菜单栏应用，帮你纠正自己写的英文，并把每一处错误记录下来，找出你最常犯的错误。

名字取自日本的金继工艺：用金漆修补破损的器物，让裂痕成为它的一部分。错误不会被抹去，而是留下来，成为进步的痕迹。

## 功能

- **随处纠错**：在任意应用中选中英文，按 `⌥⌘G` 即可检查；未选中时检查当前输入框的全部内容。
- **粘贴检查**：在无法读取选区的地方（如终端应用），先复制文字，按 `⌃⌘V` 打开面板并自动填入，按 `⌘↩` 检查，再按 `⌘↩` 复制修改后的全文。
- **就地替换**：浮窗显示修改后的全文，逐条标出错误的类型、级别和中英文解释，可一键替换原文、复制或忽略。
- **英英释义**：读到看不懂的英文时，选中后按 `⌥⌘E`，用简单的英文重述，并解释其中的难词、习语和短语动词（含语境中的意思和例句）；只选一个词或短语时，显示词典式词条：常见义项与例句、近义词、反义词、常见搭配和词族，全程不用中文。
- **错题本**：每次检查都保存在本机，可按类型、级别、时间和错误模式筛选。
- **统计概览**：错误率、错误类型分布和近 30 天高频错误模式，一眼看出薄弱环节。
- **自动更新**：通过 GitHub Releases 检查并安装新版本。

## 安装

1. 从 [Releases](https://github.com/youngle316/Kintsugi/releases/latest) 下载 `kintsugi-<版本>.dmg`（支持 Apple 芯片和 Intel），将 Kintsugi 拖入“应用程序”。
2. 首次打开时，若 macOS 提示“无法验证开发者”，前往 **系统设置 ▸ 隐私与安全性**，点击 **仍要打开**。
3. 按引导授予辅助功能权限，并填写 [SiliconFlow](https://cloud.siliconflow.cn) API Key。

系统要求：macOS 13 或更高版本。

## 隐私

检查记录和查词记录只保存在本机（`~/Library/Application Support/Kintsugi`），API Key 存放在系统钥匙串中。待检查或释义的文字仅发送给你配置的 SiliconFlow 接口。

## 从源码构建

需要 Xcode 26+、Go 1.27+ 和 Bun 1.4.4+。

```sh
bun install
bun run dev     # 运行开发版
bun run build   # 打包 .app 和 .dmg
go test ./...
```

界面基于 [MyGo](https://github.com/egoist/mygo) 和 [Lobe MyGo](https://mygo.lobehub.com) 构建。推送 `v*` 标签后，GitHub Actions 会自动构建、签名并发布新版本，更新说明取自 [CHANGELOG.md](CHANGELOG.md)。
