// Package oidc 验证 GitHub Actions 签发的 OIDC ID Token，用于上报接口的身份鉴别。
//
// 令牌由 GitHub 在 workflow run 内签发（RS256，短时效），公钥来自
// https://token.actions.githubusercontent.com/.well-known/jwks，
// keyfunc 负责拉取、缓存与密钥轮换。本包只做「令牌可信」判定；
// 令牌声明与上报载荷的绑定校验（仓库 / commit）在 service 层完成。
package oidc

import (
	"context"
	"fmt"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// GitHub Actions OIDC Provider 的签发者与 JWKS 地址。
const (
	Issuer  = "https://token.actions.githubusercontent.com"
	jwksURL = Issuer + "/.well-known/jwks"
)

// Claims 是上报鉴权关心的令牌声明子集。
type Claims struct {
	Repository string // owner/name，触发 workflow 的仓库
	SHA        string // 触发 workflow 的 commit
	Ref        string
}

// tokenClaims 是 GitHub OIDC 令牌声明的解析形状。
type tokenClaims struct {
	jwt.RegisteredClaims
	Repository string `json:"repository"`
	SHA        string `json:"sha"`
	Ref        string `json:"ref"`
}

// Verifier 校验并提取上报令牌声明；由 main 装配一次，可并发使用。
type Verifier struct {
	kf       keyfunc.Keyfunc
	audience string
}

// New 创建从 GitHub JWKS 在线取钥的验证器；ctx 控制 JWKS 后台刷新的生命周期。
func New(ctx context.Context, audience string) (*Verifier, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("创建 JWKS keyfunc: %w", err)
	}
	return NewWithKeyfunc(kf, audience), nil
}

// NewWithKeyfunc 允许注入 keyfunc（测试或自定义 JWKS 源时使用）。
func NewWithKeyfunc(kf keyfunc.Keyfunc, audience string) *Verifier {
	return &Verifier{kf: kf, audience: audience}
}

// Verify 校验令牌（RS256 签名 / iss / aud / exp），返回上报关心的声明。
// 任何失败都以 error 表达，由调用方翻译成 401。
func (v *Verifier) Verify(raw string) (Claims, error) {
	parsed, err := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}), // 防 alg 混淆攻击
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(), // 短时效令牌，缺 exp 视为不可信
	).ParseWithClaims(raw, &tokenClaims{}, v.kf.Keyfunc)
	if err != nil {
		return Claims{}, fmt.Errorf("令牌校验失败: %w", err)
	}
	cl := parsed.Claims.(*tokenClaims)
	if cl.Repository == "" || cl.SHA == "" {
		return Claims{}, fmt.Errorf("令牌缺少 repository / sha 声明")
	}
	return Claims{Repository: cl.Repository, SHA: cl.SHA, Ref: cl.Ref}, nil
}
