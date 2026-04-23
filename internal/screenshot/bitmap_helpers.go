// Package screenshot 提供位图字幕与图片压缩相关的数值辅助函数。

package screenshot

// clampJPGQScale 将 JPG qscale 限制在 ffmpeg 可接受的范围内。
func clampJPGQScale(value int) int {
	if value < 1 {
		return 1
	}
	if value > 31 {
		return 31
	}
	return value
}

// fallbackJPGQScale 为超大 JPG 重拍场景选择更保守的 qscale。
func fallbackJPGQScale(value int) int {
	value = clampJPGQScale(value)
	value += 2
	if value > 6 {
		return 6
	}
	return value
}
