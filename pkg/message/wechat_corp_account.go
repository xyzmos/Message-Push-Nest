package message

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type wechatCorpTokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type wechatCorpSendResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	InvalidUser string `json:"invaliduser,omitempty"`
}

type wechatCorpSendRequest struct {
	ToUser   string `json:"touser,omitempty"`
	MsgType  string `json:"msgtype"`
	AgentID  int    `json:"agentid"`
	Safe     int    `json:"safe,omitempty"`
	Text     *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Markdown *struct {
		Content string `json:"content"`
	} `json:"markdown,omitempty"`
	TextCard *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
	} `json:"textcard,omitempty"`
}

type wechatCorpTokenCacheItem struct {
	token     string
	expiresAt time.Time
}

var wechatCorpTokenCache = struct {
	mu sync.RWMutex
	m  map[string]wechatCorpTokenCacheItem
}{
	m: make(map[string]wechatCorpTokenCacheItem),
}

type WeChatCorpAccount struct {
	CorpID      string
	AgentID     int
	AgentSecret string
	ProxyURL    string
}

func (c *WeChatCorpAccount) cacheKey() string {
	return fmt.Sprintf("%s|%d|%s", c.CorpID, c.AgentID, c.AgentSecret)
}

func (c *WeChatCorpAccount) GetAccessToken() (string, error) {
	if c.CorpID == "" || c.AgentSecret == "" {
		return "", errors.New("企业微信应用参数缺失")
	}

	key := c.cacheKey()
	now := time.Now()

	wechatCorpTokenCache.mu.RLock()
	item, ok := wechatCorpTokenCache.m[key]
	wechatCorpTokenCache.mu.RUnlock()

	if ok && item.token != "" && now.Before(item.expiresAt) {
		return item.token, nil
	}

	token, expiresAt, err := c.refreshAccessToken()
	if err != nil {
		return "", err
	}

	wechatCorpTokenCache.mu.Lock()
	wechatCorpTokenCache.m[key] = wechatCorpTokenCacheItem{token: token, expiresAt: expiresAt}
	wechatCorpTokenCache.mu.Unlock()

	return token, nil
}

func (c *WeChatCorpAccount) refreshAccessToken() (string, time.Time, error) {
	client := c.getHTTPClient()
	reqURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(c.CorpID), url.QueryEscape(c.AgentSecret))

	resp, err := client.Get(reqURL)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, err
	}

	var res wechatCorpTokenResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", time.Time{}, err
	}
	if res.ErrCode != 0 {
		return "", time.Time{}, errors.New(res.ErrMsg)
	}
	if res.AccessToken == "" || res.ExpiresIn <= 0 {
		return "", time.Time{}, errors.New("企业微信 access_token 响应无效")
	}

	expiresAt := time.Now().Add(time.Duration(res.ExpiresIn) * time.Second).Add(-60 * time.Second)
	return res.AccessToken, expiresAt, nil
}

func (c *WeChatCorpAccount) SendText(toUser string, content string) (string, error) {
	req := wechatCorpSendRequest{
		ToUser:  toUser,
		MsgType: "text",
		AgentID: c.AgentID,
		Text: &struct {
			Content string `json:"content"`
		}{Content: content},
	}
	return c.send(req)
}

func (c *WeChatCorpAccount) SendMarkdown(toUser string, content string) (string, error) {
	req := wechatCorpSendRequest{
		ToUser:  toUser,
		MsgType: "markdown",
		AgentID: c.AgentID,
		Markdown: &struct {
			Content string `json:"content"`
		}{Content: content},
	}
	return c.send(req)
}

func (c *WeChatCorpAccount) SendTextCard(toUser, title, description, linkURL string) (string, error) {
	req := wechatCorpSendRequest{
		ToUser:  toUser,
		MsgType: "textcard",
		AgentID: c.AgentID,
		TextCard: &struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			URL         string `json:"url"`
		}{
			Title:       title,
			Description: description,
			URL:         linkURL,
		},
	}
	return c.send(req)
}

func (c *WeChatCorpAccount) send(req wechatCorpSendRequest) (string, error) {
	if req.ToUser == "" {
		return "", errors.New("企业微信应用接收者不能为空")
	}
	if c.AgentID <= 0 {
		return "", errors.New("企业微信应用 AgentID 无效")
	}

	token, err := c.GetAccessToken()
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	client := c.getHTTPClient()
	apiURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", url.QueryEscape(token))
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(b))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var res wechatCorpSendResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return string(body), err
	}
	if res.ErrCode != 0 {
		if res.InvalidUser != "" {
			return string(body), errors.New(fmt.Sprintf("%s (invaliduser=%s)", res.ErrMsg, res.InvalidUser))
		}
		return string(body), errors.New(res.ErrMsg)
	}

	return string(body), nil
}

func (c *WeChatCorpAccount) getHTTPClient() *http.Client {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	if c.ProxyURL == "" {
		return client
	}

	proxyURL, err := url.Parse(c.ProxyURL)
	if err != nil {
		return client
	}

	if strings.HasPrefix(strings.ToLower(c.ProxyURL), "socks5://") {
		dialer, err := c.createSOCKS5Dialer(proxyURL)
		if err != nil {
			return client
		}
		client.Transport = &http.Transport{
			DialContext: dialer.DialContext,
		}
		return client
	}

	client.Transport = &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	return client
}

func (c *WeChatCorpAccount) createSOCKS5Dialer(proxyURL *url.URL) (proxy.ContextDialer, error) {
	host := proxyURL.Host

	var auth *proxy.Auth
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		auth = &proxy.Auth{
			User:     proxyURL.User.Username(),
			Password: password,
		}
	}

	baseDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	dialer, err := proxy.SOCKS5("tcp", host, auth, baseDialer)
	if err != nil {
		return nil, err
	}

	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("failed to convert to ContextDialer")
	}

	return contextDialer, nil
}

