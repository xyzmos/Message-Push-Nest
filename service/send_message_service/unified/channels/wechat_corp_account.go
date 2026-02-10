package channels

import (
	"fmt"
	"message-nest/models"
	"message-nest/pkg/message"
	"message-nest/service/send_ins_service"
	"message-nest/service/send_way_service"
	"strings"
)

type WeChatCorpAccountChannel struct{ *BaseChannel }

func NewWeChatCorpAccountChannel() *WeChatCorpAccountChannel {
	return &WeChatCorpAccountChannel{BaseChannel: NewBaseChannel(MessageTypeWeChatCorpAccount, []string{FormatTypeMarkdown, FormatTypeText})}
}

func (c *WeChatCorpAccountChannel) SendUnified(msgObj interface{}, ins models.SendTasksIns, content *UnifiedMessageContent) (string, string) {
	auth, ok := msgObj.(*send_way_service.WayDetailWeChatCorpAccount)
	if !ok {
		return "", "类型转换失败"
	}

	insService := send_ins_service.SendTaskInsService{}
	errStr, configInterface := insService.ValidateDiffIns(ins)
	if errStr != "" {
		return errStr, ""
	}
	config, ok := configInterface.(models.InsWeChatCorpAccountConfig)
	if !ok {
		return "企业微信应用config校验失败", ""
	}

	contentType, formattedContent, err := c.FormatContent(content)
	if err != nil {
		return "", err.Error()
	}

	toUser := config.ToAccount
	if content.IsAtAll() {
		toUser = "@all"
	} else {
		atUserIds := content.GetAtUserIds()
		if len(atUserIds) > 0 {
			toUser = strings.Join(atUserIds, "|")
		}
	}

	cli := message.WeChatCorpAccount{
		CorpID:      auth.CorpID,
		AgentID:     auth.AgentID,
		AgentSecret: auth.AgentSecret,
		ProxyURL:    auth.ProxyURL,
	}

	var res string
	var sendErr error
	if contentType == FormatTypeMarkdown {
		res, sendErr = cli.SendMarkdown(toUser, formattedContent)
	} else if contentType == FormatTypeText {
		if content.Title != "" && content.URL != "" {
			res, sendErr = cli.SendTextCard(toUser, content.Title, formattedContent, content.URL)
		} else {
			res, sendErr = cli.SendText(toUser, formattedContent)
		}
	} else {
		sendErr = fmt.Errorf("未知的企业微信应用发送内容类型：%s", contentType)
	}

	var errMsg string
	if sendErr != nil {
		errMsg = fmt.Sprintf("发送失败：%s", sendErr.Error())
	}
	return res, errMsg
}

