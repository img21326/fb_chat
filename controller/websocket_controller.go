package controller

import (
	"context"
	"log"
	"net/http"
	"strconv"

	messageStruct "github.com/img21326/fb_chat/structure/message"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/img21326/fb_chat/helper"
	pubmessage "github.com/img21326/fb_chat/structure/pub_message"
	"github.com/img21326/fb_chat/structure/user"
	"github.com/img21326/fb_chat/usecase/message"
	"github.com/img21326/fb_chat/usecase/pair"
	"github.com/img21326/fb_chat/usecase/sub"
	"github.com/img21326/fb_chat/usecase/ws"
	"github.com/img21326/fb_chat/ws/client"
	"gorm.io/gorm"
)

type WebsocketController struct {
	WSUpgrader     websocket.Upgrader
	WsUsecase      ws.WebsocketUsecaseInterface
	SubUscase      sub.SubMessageUsecaseInterface
	PairUsecase    pair.PairUsecaseInterface
	MessageUsecase message.MessageUsecaseInterface
	PubMessageChan chan *pubmessage.PublishMessage
}

func NewWebsocketController(e gin.IRoutes,
	wsUsecase ws.WebsocketUsecaseInterface,
	subUsecase sub.SubMessageUsecaseInterface,
	pairUsecase pair.PairUsecaseInterface,
	messageUsecase message.MessageUsecaseInterface,
) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	publishMessageChan := make(chan *pubmessage.PublishMessage, 1024)
	pairUsecase.SetMessageChan(publishMessageChan)

	saveMessageChan := make(chan *messageStruct.Message, 1024)
	messageUsecase.SetMessageChan(saveMessageChan)
	wsUsecase.SetSaveMessageChan(saveMessageChan)

	controller := WebsocketController{
		WSUpgrader:     upgrader,
		WsUsecase:      wsUsecase,
		SubUscase:      subUsecase,
		PairUsecase:    pairUsecase,
		MessageUsecase: messageUsecase,
		PubMessageChan: publishMessageChan,
	}

	ctx := context.Background()
	go controller.SubUscase.Subscribe(ctx, "message", controller.WsUsecase.ReceiveMessage)
	go controller.SubUscase.Publish(ctx, "message", publishMessageChan)
	go controller.WsUsecase.Run(ctx)
	go controller.PairUsecase.Run(ctx)
	go controller.MessageUsecase.Run(ctx)

	e.GET("/ws", controller.WS)
}

func (c *WebsocketController) WS(ctx *gin.Context) {
	token := ctx.Query("token")
	id, _ := strconv.Atoi(token)
	// user, err := c.AuthUsecase.VerifyToken(token)
	// if err != nil {
	// 	log.Printf("token error: %v", err)
	// 	return
	// }

	m := gorm.Model{
		ID: uint(id),
	}
	user := &user.User{
		Model:  m,
		FbID:   helper.RandString(16),
		Name:   helper.RandString(5),
		Gender: "male",
	}
	log.Printf("new ws connection: %v", user.Name)
	room, err := c.WsUsecase.FindRoomByUserId(ctx, user.ID)
	if err != nil && err != gorm.ErrRecordNotFound && err.Error() != "RoomIsClosed" {
		log.Printf("find room error: %v", err)
		return
	}
	conn, err := c.WSUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		log.Printf("ws error: %v", err)
		return
	}
	contextBackground, cancel := context.WithCancel(context.Background())
	client := client.Client{
		Conn:      conn,
		Send:      make(chan []byte, 256),
		User:      *user,
		Ctx:       contextBackground,
		CtxCancel: cancel,
	}
	c.WsUsecase.Register(&client)
	if room != nil {
		log.Printf("new ws connection: %v in room %v", user.Name, room.ID)
		client.RoomId = room.ID
		if room.UserId1 == client.User.ID {
			client.PairId = room.UserId2
		} else {
			client.PairId = room.UserId1
		}
		client.Send <- []byte("{'type': 'inRoom'}")
	} else {
		log.Printf("new ws connection: %v with new pairing", user.Name)
		want, ok := ctx.GetQuery("want")
		if !ok {
			log.Printf("ws not set want param")
			client.Conn.Close()
			return
		}
		client.WantToFind = want
		c.PairUsecase.Add(&client)
		client.Send <- []byte("{'type': 'paring'}")
	}

	go client.ReadPump(c.PubMessageChan)
	go client.WritePump()

}
