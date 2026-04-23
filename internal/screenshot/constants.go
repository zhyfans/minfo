// Package screenshot 定义截图流程共用的常量和静态匹配规则。

package screenshot

import "regexp"

const (
	defaultSubtitleDuration = 4.0
	subtitleSnapEpsilon     = 0.50
	playlistScanMax         = 6
	oversizeBytes           = 10 * 1024 * 1024
)

var (
	langZHHansTokens = []string{"简体", "简中", "chs", "zh-hans", "zh_hans", "zh-cn", "zh_cn"}
	langZHHantTokens = []string{"繁体", "繁中", "cht", "big5", "zh-hant", "zh_hant", "zh-tw", "zh_tw"}
	langZHTokens     = []string{"中文", "chinese", "zho", "chi", "zh"}
	langENTokens     = []string{"en", "eng", "english"}

	clipIDPattern = regexp.MustCompile(`[0-9]{5}M2TS`)
)
