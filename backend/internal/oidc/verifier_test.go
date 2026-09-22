package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// testSigner 用测试生成的 RSA 密钥模拟 GitHub 的 OIDC Provider。
type testSigner struct {
	key *rsa.PrivateKey
	kid string
}

func newTestSigner(t *testing.T) *testSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥: %v", err)
	}
	return &testSigner{key: key, kid: "test-key-1"}
}

// jwksJSON 把公钥编码成 JWK Set（与 GitHub JWKS 形状一致）。
func (s *testSigner) jwksJSON() json.RawMessage {
	pub := s.key.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	raw, err := json.Marshal(map[string]any{
		"keys": []map[string]string{
			{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": s.kid, "n": n, "e": e},
		},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// sign 按给定声明签发 RS256 令牌。
func (s *testSigner) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = s.kid
	raw, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatalf("签发令牌: %v", err)
	}
	return raw
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":        Issuer,
		"aud":        []string{"homework-center"},
		"exp":        time.Now().Add(5 * time.Minute).Unix(),
		"iat":        time.Now().Unix(),
		"repository": "zhangsan/redrock_backend_practice_2026",
		"sha":        "9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234",
		"ref":        "refs/heads/main",
	}
}

func newTestVerifier(t *testing.T, s *testSigner) *Verifier {
	t.Helper()
	kf, err := keyfunc.NewJWKSetJSON(s.jwksJSON())
	if err != nil {
		t.Fatalf("构造测试 keyfunc: %v", err)
	}
	return NewWithKeyfunc(kf, "homework-center")
}

func TestVerifyAcceptsWellFormedToken(t *testing.T) {
	s := newTestSigner(t)
	v := newTestVerifier(t, s)

	cl, err := v.Verify(s.sign(t, validClaims()))
	if err != nil {
		t.Fatalf("合法令牌应通过: %v", err)
	}
	if cl.Repository != "zhangsan/redrock_backend_practice_2026" ||
		cl.SHA != "9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234" ||
		cl.Ref != "refs/heads/main" {
		t.Fatalf("声明提取不符: %+v", cl)
	}
}

func TestVerifyRejects(t *testing.T) {
	s := newTestSigner(t)
	v := newTestVerifier(t, s)

	bad := func(name string, claims jwt.MapClaims) {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(s.sign(t, claims)); err == nil {
				t.Fatalf("%s 应被拒绝", name)
			}
		})
	}

	c := func(mutate func(jwt.MapClaims)) jwt.MapClaims {
		m := validClaims()
		mutate(m)
		return m
	}

	bad("iss 不符", c(func(m jwt.MapClaims) { m["iss"] = "https://evil.example.com" }))
	bad("aud 不符", c(func(m jwt.MapClaims) { m["aud"] = []string{"other-service"} }))
	bad("已过期", c(func(m jwt.MapClaims) { m["exp"] = time.Now().Add(-time.Minute).Unix() }))
	bad("缺 exp", c(func(m jwt.MapClaims) { delete(m, "exp") }))
	bad("缺 repository", c(func(m jwt.MapClaims) { delete(m, "repository") }))
	bad("缺 sha", c(func(m jwt.MapClaims) { delete(m, "sha") }))

	// kid 未知：header 的 kid 与 JWKS 不匹配
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, validClaims())
	tok.Header["kid"] = "unknown-kid"
	raw, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatalf("签发: %v", err)
	}
	if _, err := v.Verify(raw); err == nil {
		t.Fatalf("未知 kid 应被拒绝")
	}

	// alg 混淆攻击：HS256 + 把 RSA 公钥当 HMAC 密钥
	hmacTok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
	pubDER, _ := json.Marshal(s.key.PublicKey)
	signed, err := hmacTok.SignedString(pubDER)
	if err != nil {
		t.Fatalf("签发 HS256: %v", err)
	}
	if _, err := v.Verify(signed); err == nil {
		t.Fatalf("HS256 令牌应被拒绝")
	}

	// 签名被篡改
	raw = s.sign(t, validClaims())
	parts := strings.Split(raw, ".")
	if _, err := v.Verify(fmt.Sprintf("%s.%s.%s", parts[0], parts[1], "AAAA"+parts[2])); err == nil {
		t.Fatalf("被篡改的签名应被拒绝")
	}
}
