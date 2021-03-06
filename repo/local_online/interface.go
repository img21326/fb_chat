package localonline

import (
	"github.com/img21326/fb_chat/ws/client"
)

type LocalOnlineRepoInterface interface {
	Register(client *client.Client)
	UnRegister(client *client.Client)
	FindUserByID(userId uint) (*client.Client, error)
}
