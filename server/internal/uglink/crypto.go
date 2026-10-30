package uglink

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// validUglinkID 校验 UGREENlink ID（小写字母数字与连字符）
var uglinkIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validUglinkID(id string) bool {
	return uglinkIDPattern.MatchString(id)
}

// encryptWithPublicKey 用 Base64 编码的 PEM 公钥做 RSA PKCS#1 v1.5 加密。
// 绿联登录接口要求密码与 token 都以此方式加密（参考实现用 JSEncrypt，等价于 PKCS#1 v1.5）。
func encryptWithPublicKey(plaintext, encodedKey string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		// 也接受直接传 PEM 的情况
		der = []byte(encodedKey)
	}
	block, _ := pem.Decode(der)
	if block == nil {
		// 可能是不带 PEM 头尾的裸 DER
		block = &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	}
	var pub *rsa.PublicKey
	if parsed, parseErr := x509.ParsePKIXPublicKey(block.Bytes); parseErr == nil {
		rsaPub, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return "", errors.New("公钥不是 RSA 类型")
		}
		pub = rsaPub
	} else if parsed, parseErr := x509.ParsePKCS1PublicKey(block.Bytes); parseErr == nil {
		pub = parsed
	} else {
		return "", fmt.Errorf("公钥解析失败：%w", parseErr)
	}
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// md5Hex 绿联用 md5(token) 作 X-Ugreen-Security-Key（非安全用途，仅为协议要求）。
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// clientID 生成 UG-Client-Id：稳定且唯一即可，用用户名哈希避免多账号互相干扰。
func clientID(username string) string {
	sum := sha256.Sum256([]byte("lovelife-uglink-" + username))
	return hex.EncodeToString(sum[:8])
}

// setBrowserHeaders 补齐浏览器上下文头，绿联侧据此判定来源合法。
func setBrowserHeaders(req *http.Request, loginOrigin string) {
	req.Header.Set("Origin", loginOrigin)
	req.Header.Set("Referer", loginOrigin+"/desktop/")
	req.Header.Set("User-Agent", userAgent)
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// setCookieHeader 把已知 cookie 写入请求头。
// 不做按域过滤：本包只在登录源与中转域之间传递同一批 cookie（两者同源），
// 加域匹配反而会漏掉绿联设在不同子路径上的凭据。
func setCookieHeader(req *http.Request, cookies []*http.Cookie) {
	jar := map[string]string{}
	for _, c := range cookies {
		jar[c.Name] = c.Value
	}
	if h := cookieHeader(jar); h != "" {
		req.Header.Set("Cookie", h)
	}
}

// cookieHeader 把 cookie map 拼成请求头值。
func cookieHeader(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

// cookiesFromHTML 从页面内的 document.cookie 赋值语句提取 cookie。
// 参考实现用正则扫 `document.cookie = "k=v; ..."` 与 `document.cookie='k=v'` 两种写法。
var (
	docCookieDouble = regexp.MustCompile(`document\.cookie\s*=\s*"([^"]+)"`)
	docCookieSingle = regexp.MustCompile(`document\.cookie\s*=\s*'([^']+)'`)
)

func cookiesFromHTML(html string) map[string]string {
	out := map[string]string{}
	for _, re := range []*regexp.Regexp{docCookieDouble, docCookieSingle} {
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) < 2 {
				continue
			}
			for _, pair := range strings.Split(m[1], ";") {
				pair = strings.TrimSpace(pair)
				idx := strings.Index(pair, "=")
				if idx <= 0 {
					continue
				}
				out[pair[:idx]] = pair[idx+1:]
			}
		}
	}
	return out
}
