package open

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getsentry/sentry-go"
	"github.com/mrwangjinjin/go-wechat/core"
	"github.com/mrwangjinjin/go-wechat/pkg/util"
	"net/http"
	"net/url"
	"time"
)

func init() {
	sentry.Init(sentry.ClientOptions{
		Dsn: "http://23f4952429544a4ea9fd98e9173a9443@sentry.lianyunapp.cn/15",
	})
}

const (
	ComponentTicketCacheKeyPrefix = "CACHE_TICKET@@"
	ComponentTokenCacheKeyPrefix  = "CACHE_COMPONENT@@"
	AuthorizerTokenCacheKeyPrefix = "CACHE_AUTHORIZER_TOKEN@@"
)

//

type Client struct {
	Http      *core.HttpClient
	Endpoint  *core.Endpoint
	Cache     core.Cache
	AppId     string
	AppSecret string
	Token     string
	AesKey    string
}

// NewClient
func NewClient(clientConfig *core.ClientConfig, cache core.Cache) *Client {
	return &Client{
		Http:      core.NewHttpClient(),
		Cache:     cache,
		Endpoint:  core.NewEndpoint(clientConfig.BaseUrl),
		AppId:     clientConfig.AppId,
		AppSecret: clientConfig.AppSecret,
		Token:     clientConfig.Token,
		AesKey:    clientConfig.AesKey,
	}
}

// GetAuthUrl 获取授权页网址
func (self *Client) GetAuthUrl(redirectUri string, authType uint8) string {
	preAuthCode, err := self.ApiCreatePreAuthCode()
	if err != nil {
		sentry.CaptureException(err)
		return ""
	}
	return fmt.Sprintf("https://mp.weixin.qq.com/cgi-bin/componentloginpage?component_appid=%s&pre_auth_code=%s&redirect_uri=%s&auth_type=%d",
		url.QueryEscape(self.AppId),
		url.QueryEscape(preAuthCode),
		url.QueryEscape(redirectUri),
		authType)
}

// GetToken
func (self *Client) GetToken() (map[string]interface{}, error) {
	resp, err := self.Cache.Get(AuthorizerTokenCacheKeyPrefix + self.AppId)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	return util.JsonUnmarshal(string(resp)), nil
}

// RefreshToken
func (self *Client) RefreshToken(authorizerAppId, refreshToken string) (map[string]interface{}, error) {
	resp, err := self.Cache.Get(AuthorizerTokenCacheKeyPrefix + self.AppId)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	authorizerToken := util.JsonUnmarshalBytes(resp)
	dst, err := json.Marshal(map[string]interface{}{
		"component_appid":          self.AppId,
		"authorizer_appid":         authorizerAppId,
		"authorizer_refresh_token": refreshToken,
	})
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	status, body, err := self.Http.Post(self.Endpoint.ApiAuthorizerToken(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return nil, errors.New("网络错误")
	}
	authorizerRefreshToken := util.JsonUnmarshalBytes(body)
	authorizerToken["authorizer_access_token"] = authorizerRefreshToken["authorizer_access_token"]
	authorizerToken["authorizer_refresh_token"] = authorizerRefreshToken["authorizer_refresh_token"]
	authorizerToken["expires_in"] = time.Now().Unix() + 7200
	_ = self.Cache.SetEx(AuthorizerTokenCacheKeyPrefix+self.AppId, authorizerToken, 7200)
	return util.JsonUnmarshalBytes(body), nil
}

// ApiCreatePreAuthCode 获取预授权码
func (self *Client) ApiCreatePreAuthCode() (string, error) {
	dst, err := json.Marshal(map[string]interface{}{
		"component_appid": self.AppId,
	})
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	status, body, err := self.Http.Post(self.Endpoint.PreAuthCodoUrl(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	if status != http.StatusOK {
		sentry.CaptureMessage("网络错误")
		return "", errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)

	return resp["pre_auth_code"].(string), nil
}

// ApiQueryAuth 使用授权码换取公众号或小程序的接口调用凭据和授权信息
func (self *Client) ApiQueryAuth(code string) (map[string]interface{}, error) {
	exist := self.Cache.Exists(AuthorizerTokenCacheKeyPrefix + self.AppId)
	if !exist {
		authorizerToken, err := self.getRawApiQueryAuth(code)
		if err != nil {
			sentry.CaptureException(err)
			return authorizerToken, err
		}
		return authorizerToken, nil
	}
	resp, err := self.Cache.Get(AuthorizerTokenCacheKeyPrefix + self.AppId)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	authorizerToken := util.JsonUnmarshalBytes(resp)
	if time.Now().Unix() > int64(authorizerToken["expires_in"].(float64)) {
		authorizerToken, err := self.getRawApiQueryAuth(code)
		if err != nil {
			sentry.CaptureException(err)
			return authorizerToken, err
		}
		return authorizerToken, nil
	}
	return authorizerToken, nil
}

// ApiQueryAuth 使用授权码换取公众号或小程序的接口调用凭据和授权信息
func (self *Client) getRawApiQueryAuth(code string) (map[string]interface{}, error) {
	dst, err := json.Marshal(map[string]interface{}{
		"component_appid":    self.AppId,
		"authorization_code": code,
	})
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	status, body, err := self.Http.Post(self.Endpoint.ApiQueryAuth(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	if status != http.StatusOK {
		sentry.CaptureMessage("网络错误")
		return nil, errors.New("网络错误")
	}
	authorizerToken := util.JsonUnmarshalBytes(body)
	authorzationInfo := authorizerToken["authorization_info"].(map[string]interface{})
	authorzationInfo["expires_in"] = time.Now().Unix() + 7200
	err = self.Cache.SetEx(AuthorizerTokenCacheKeyPrefix+self.AppId, authorzationInfo, 7200)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	return util.JsonUnmarshalBytes(body), nil
}

// ApiAuthorizerInfo 获取授权方的帐号基本信息
func (self *Client) ApiAuthorizerInfo(authorizerAppId string) (map[string]interface{}, error) {
	dst, err := json.Marshal(map[string]interface{}{
		"component_appid":  self.AppId,
		"authorizer_appid": authorizerAppId,
	})
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	status, body, err := self.Http.Post(self.Endpoint.ApiAuthorizerInfo(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return nil, errors.New("网络错误")
	}
	authorizerToken := util.JsonUnmarshalBytes(body)
	if authorizerToken == nil {
		sentry.CaptureException(errors.New("ApiAuthorizerInfo：数据包不正确"))
	}
	authorizerInfo := authorizerToken["authorizer_info"].(map[string]interface{})
	return authorizerInfo, nil
}

// ApiComponentToken 获取第三方平台component_access_token
func (self *Client) ApiComponentToken() (string, error) {
	exist := self.Cache.Exists(ComponentTokenCacheKeyPrefix + self.AppId)
	if !exist {
		componentToken, err := self.getRawApiComponentToken()
		if err != nil {
			sentry.CaptureException(err)
			return "", err
		}
		return componentToken["component_access_token"].(string), nil
	}
	resp, err := self.Cache.Get(ComponentTokenCacheKeyPrefix + self.AppId)
	if err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	componentToken := util.JsonUnmarshalBytes(resp)
	if time.Now().Unix() > int64(componentToken["expires_in"].(float64)) {
		componentToken, err := self.getRawApiComponentToken()
		if err != nil {
			sentry.CaptureException(err)
			return "", err
		}
		return componentToken["component_access_token"].(string), nil
	}
	return componentToken["component_access_token"].(string), nil
}

// getRawApiComponentToken 获取第三方平台component_access_token
func (self *Client) getRawApiComponentToken() (map[string]interface{}, error) {
	dst, err := json.Marshal(map[string]interface{}{
		"component_appid":         self.AppId,
		"component_appsecret":     self.AppSecret,
		"component_verify_ticket": self.getComponentTicket(),
	})
	status, body, err := self.Http.Post(self.Endpoint.ComponentAccessTokenUrl(), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	if status != http.StatusOK {
		sentry.CaptureException(err)
		return nil, err
	}
	componentToken := util.JsonUnmarshalBytes(body)
	componentToken["expires_in"] = time.Now().Unix() + 7200
	_ = self.Cache.SetEx(ComponentTokenCacheKeyPrefix+self.AppId, componentToken, 7200)
	return componentToken, nil
}

// getComponentTicket 获取component_verify_ticket
func (self *Client) getComponentTicket() (ticket string) {
	exist := self.Cache.Exists(ComponentTicketCacheKeyPrefix + self.AppId)
	if !exist {
		sentry.CaptureMessage(ComponentTicketCacheKeyPrefix + self.AppId + "缓存未命中")
		return ""
	}
	resp, _ := self.Cache.Get(ComponentTicketCacheKeyPrefix + self.AppId)
	componentVerifyTicket := util.JsonUnmarshalBytes(resp)
	return string(componentVerifyTicket["component_verify_ticket"].(string))
}

// FastRegisterWeapp 快速注册小程序
func (self *Client) FastRegisterWeapp(data map[string]interface{}) error {
	dst, err := json.Marshal(data)
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.FastRegisterWeapp(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("注册失败")
	}

	return nil
}

// BindTester 绑定体验者账号
func (self *Client) BindTester(wechatId string) error {
	dst, err := json.Marshal(map[string]interface{}{
		"wechatid": wechatId,
	})
	token, err := self.GetToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.BindTester(token["authorizer_access_token"].(string)), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("操作失败")
	}
	return nil
}

// ModifyDomain 修改小程序服务器域名
func (self *Client) ModifyDomain(data map[string]interface{}) error {
	dst, err := json.Marshal(data)
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.ModifyDomain(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("操作失败")
	}
	return nil
}

// CommitCode 上传小程序代码
func (self *Client) CommitCode(data map[string]interface{}) error {
	dst, err := json.Marshal(data)
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.CommitCode(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("操作失败")
	}
	return nil
}

// SubmitAudit 提交审核
func (self *Client) SubmitAudit(data map[string]interface{}) error {
	dst, err := json.Marshal(data)
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.SubmitAudit(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("操作失败")
	}
	return nil
}

// UndoCodeAudit 审核撤回
func (self *Client) UndoCodeAudit(data map[string]interface{}) error {
	dst, err := json.Marshal(data)
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.SubmitAudit(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("操作失败")
	}
	return nil
}

// Release 小程序发布
func (self *Client) Release(data map[string]interface{}) error {
	dst, err := json.Marshal(data)
	token, err := self.ApiComponentToken()
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	status, body, err := self.Http.Post(self.Endpoint.Release(token), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if resp["errcode"].(int64) != 0 {
		sentry.CaptureException(errors.New("接口错误"))
		return errors.New("操作失败")
	}
	return nil
}

// GetWxaCode 小程序码
func (self *Client) GetWxaCode(data map[string]interface{}) ([]byte, error) {
	dst, err := json.Marshal(data)
	token, err := self.GetToken()
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	status, body, err := self.Http.Post(self.Endpoint.GetWxaCode(token["authorizer_access_token"].(string)), "application/json", dst)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	if status != http.StatusOK {
		sentry.CaptureException(errors.New("网络错误"))
		return nil, errors.New("网络错误")
	}
	resp := util.JsonUnmarshalBytes(body)
	if _, ok := resp["errcode"]; ok {
		sentry.CaptureException(errors.New("接口错误"))
		return nil, errors.New("操作失败")
	}
	return body, nil
}
