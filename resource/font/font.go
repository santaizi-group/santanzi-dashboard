package font

import (
	_ "embed"

	"golang.org/x/image/font/gofont/goregular"
)

//go:embed santaizi-cjk.ttf
var cjk []byte

func TTF() []byte {
	if len(cjk) > 2048 {
		return cjk
	}
	return goregular.TTF
}

func HasCJK() bool {
	// Go Regular 约 146KiB。真实 Noto Sans SC 子集通常更大；未替换时图内中文回退 ASCII。
	return len(cjk) > 400000
}
