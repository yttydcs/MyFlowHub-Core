# 字体说明

为了在 Windows / macOS / Linux 上正确显示中文，debugclient 会尝试加载系统字体：

- Windows: `msyh.ttc/msyh.ttf`、`simhei.ttf` 等
- macOS: `STHeiti`、`Hiragino Sans GB`
- Linux: `WenQuanYi`、`Noto Sans CJK`

请确保系统中至少安装一种上述字体。后续如需内置字体，可将对应 TTF 文件放到项目中并更新 `theme.Apply` 的加载逻辑。

