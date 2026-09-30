package jm

import (
	"fmt"
	"image"
)

// getNum 计算图片分割数(SDK jm_toolkit.py:905-926 直译):
//
//	aid < scramble_id            → 0(无需乱序)
//	aid < 268850                 → 10
//	否则                          → md5("{aid}{filename}") 末字符 % x * 2 + 2,x = 10(aid<421926) 或 8
func getNum(scrambleID, aid int64, filename string) int {
	if aid < scrambleID {
		return 0
	}
	if aid < scrambleThreshold268850 {
		return 10
	}
	x := 10
	if aid >= scrambleThreshold421926 {
		x = 8
	}
	s := md5Hex(fmt.Sprintf("%d%s", aid, filename))
	num := int(s[len(s)-1]) % x
	return num*2 + 2
}

// descrambleImage 等价于 SDK JmImageTool.decode_and_save(jm_toolkit.py:845-896):
// 水平切割为 num 条并按倒序重排;num==0 原样返回。
// 数学语义(与 Python 逐行对应):
//
//	over = h % num
//	move = floor(h / num)
//	for i in 0..num-1:
//	    y_src = h - move*(i+1) - over
//	    y_dst = move*i
//	    i==0: move += over   else: y_dst += over
//	    dst[y_dst : y_dst+move] = src[y_src : y_src+move]
func descrambleImage(src image.Image, num int) image.Image {
	if num <= 0 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	over := h % num
	move := h / num
	for i := 0; i < num; i++ {
		ySrc := h - move*(i+1) - over
		yDst := move * i
		m := move
		if i == 0 {
			m += over
		} else {
			yDst += over
		}
		for y := 0; y < m; y++ {
			for x := 0; x < w; x++ {
				dst.Set(x, yDst+y, src.At(b.Min.X+x, b.Min.Y+ySrc+y))
			}
		}
	}
	return dst
}
