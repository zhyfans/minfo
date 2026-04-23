package runtime

import "strings"

// VariantSettingsFor 会根据输出格式选择对应的探测、搜索和编码参数。
func VariantSettingsFor(variant string) VariantSettings {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "jpg":
		return VariantSettings{
			Ext:            ".jpg",
			ProbeSize:      "100M",
			Analyze:        "100M",
			CoarseBackText: 2,
			CoarseBackPGS:  8,
			RenderBackText: 1,
			RenderBackPGS:  2,
			SearchBack:     4,
			SearchForward:  8,
			JPGQuality:     1,
		}
	default:
		return VariantSettings{
			Ext:            ".png",
			ProbeSize:      "150M",
			Analyze:        "150M",
			CoarseBackText: 3,
			CoarseBackPGS:  12,
			RenderBackText: 1,
			RenderBackPGS:  2,
			SearchBack:     6,
			SearchForward:  10,
			JPGQuality:     85,
		}
	}
}
