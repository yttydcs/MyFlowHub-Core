package theme

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"
)

// Apply 尝试为 Fyne 应用启用支持中文的字体，避免界面出现乱码。
func Apply(app fyne.App) {
	if app == nil {
		return
	}
	fontRes, fontPath := loadFontResource()
	if fontRes == nil {
		return
	}
	app.Settings().SetTheme(newCustomTheme(fontRes))
	fmt.Printf("[debugclient] 使用字体: %s\n", fontPath)
}

type customTheme struct {
	base fyne.Theme
	font fyne.Resource
}

func newCustomTheme(font fyne.Resource) fyne.Theme {
	if font == nil {
		return fynetheme.DefaultTheme()
	}
	return &customTheme{base: fynetheme.DefaultTheme(), font: font}
}

func (t *customTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return t.base.Color(name, variant)
}

func (t *customTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *customTheme) Font(style fyne.TextStyle) fyne.Resource {
	if t.font != nil {
		return t.font
	}
	return t.base.Font(style)
}

func (t *customTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}

func loadFontResource() (fyne.Resource, string) {
	for _, path := range candidateFontPaths() {
		if path == "" {
			continue
		}
		if !strings.EqualFold(filepath.Ext(path), ".ttf") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		res := fyne.NewStaticResource(filepath.Base(path), data)
		return res, path
	}
	return nil, ""
}

func candidateFontPaths() []string {
	switch runtime.GOOS {
	case "windows":
		winDir := os.Getenv("WINDIR")
		fontsDir := filepath.Join(winDir, "Fonts")
		return []string{
			filepath.Join(fontsDir, "simhei.ttf"),
			filepath.Join(fontsDir, "simfang.ttf"),
			filepath.Join(fontsDir, "simkai.ttf"),
			filepath.Join(fontsDir, "msyh.ttf"),
			filepath.Join(fontsDir, "simsun.ttf"),
		}
	case "darwin":
		return []string{
			"/System/Library/Fonts/STHeiti Light.ttc",
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/Hiragino Sans GB W3.ttc",
		}
	default:
		return []string{
			"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		}
	}
}
