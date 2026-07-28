package wechat_mp

import (
	"encoding/xml"
)

// WXMessage 微信公众号服务器推送的 XML 消息结构（仅关注事件部分字段）
type WXMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"` // 发送方 openid
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`  // "event"
	Event        string   `xml:"Event"`   // "subscribe" / "SCAN"
	EventKey     string   `xml:"EventKey"` // subscribe: "qrscene_xxx"；SCAN: "xxx"
	Ticket       string   `xml:"Ticket"`
}

// MpLoginState 公众号扫码登录会话状态（序列化为 JSON 存入 Redis）
type MpLoginState struct {
	SceneId   string `json:"scene_id"`
	Status    string `json:"status"` // pending / scanned / logged_in / failed
	OpenID    string `json:"openid"`
	Nickname  string `json:"nickname"`
	UserID    int    `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
	AffCode   string `json:"aff_code"` // 邀请码（生成二维码时携带）
}

// 微信 API 响应结构
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

type qrcodeResponse struct {
	Ticket        string `json:"ticket"`
	ExpireSeconds int    `json:"expire_seconds"`
	Url           string `json:"url"`
	ErrCode       int    `json:"errcode"`
	ErrMsg        string `json:"errmsg"`
}

// UserInfo 微信用户基本信息（昵称等）
type UserInfo struct {
	OpenID   string `json:"openid"`
	Nickname string `json:"nickname"`
	Sex      int    `json:"sex"`
	ErrCode  int    `json:"errcode"`
	ErrMsg   string `json:"errmsg"`
}

// 公众号扫码登录会话状态常量
const (
	StatusPending  = "pending"
	StatusScanned  = "scanned"
	StatusLoggedIn = "logged_in"
	StatusFailed   = "failed"
)
