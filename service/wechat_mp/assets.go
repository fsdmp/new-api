package wechat_mp

import _ "embed"

// groupQrCodeImage 内嵌的官方交流群二维码图片（JPEG）。
// 关注公众号后通过客服消息推送给用户，引导其扫码加入交流群。
//
//go:embed assets/group_qrcode.jpg
var groupQrCodeImage []byte

// GroupQRCodeImage 返回内嵌的群二维码图片字节。
func GroupQRCodeImage() []byte {
	return groupQrCodeImage
}
